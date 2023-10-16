package repositories

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"net"
	"net/http"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/models/dto"
	"sync"
	"time"
)

type Track struct {
	OwnerId    uint64
	TrackLocal *webrtc.TrackLocalStaticRTP
}

type Peer struct {
	ID                     uint64
	Conn                   *webrtc.PeerConnection
	CanPublish             bool
	IsCaller               bool
	HandshakeLock          *sync.Mutex
	gotFirstVideoTrack     bool
	gotFirstAudioTrack     bool
	triggeredReconnectOnce bool
}

type Room struct {
	*sync.Mutex
	Peers     map[uint64]*Peer
	trackLock *sync.Mutex
	Tracks    map[string]*Track
	timer     *time.Ticker
}

type RoomRepository struct {
	api   *webrtc.API
	Rooms map[string]*Room
	conf  *models.ConfigModel
	*sync.Mutex
}

func NewRoomRepository(conf *models.ConfigModel) *RoomRepository {
	settingEngine := webrtc.SettingEngine{}
	if len(conf.CustomICEHostCandidateIP) > 0 {
		settingEngine.SetNAT1To1IPs([]string{conf.CustomICEHostCandidateIP}, webrtc.ICECandidateTypeHost)
	}
	settingEngine.SetNetworkTypes([]webrtc.NetworkType{
		webrtc.NetworkTypeTCP6,
		webrtc.NetworkTypeUDP6,
		webrtc.NetworkTypeTCP4,
		webrtc.NetworkTypeUDP4,
	})
	tcpListener, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IP{0, 0, 0, 0},
		Port: int(conf.ICETCPMUXListenPort),
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Listening for ICE TCP at %s\n", tcpListener.Addr())

	tcpMux := webrtc.NewICETCPMux(nil, tcpListener, 64)
	settingEngine.SetICETCPMux(tcpMux)

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		panic(err)
	}

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i), webrtc.WithSettingEngine(settingEngine))

	return &RoomRepository{
		api:   api,
		Mutex: &sync.Mutex{},
		Rooms: make(map[string]*Room),
		conf:  conf,
	}
}

func (r *RoomRepository) DoesRoomExists(id string) bool {
	r.Lock()
	defer r.Unlock()
	return r.doesRoomExists(id)
}

func (r *RoomRepository) doesRoomExists(id string) bool {
	if _, exists := r.Rooms[id]; exists {
		return true
	}
	return false
}

func (r *RoomRepository) doesPeerExists(roomId string, id uint64) bool {
	if !r.doesRoomExists(roomId) {
		return false
	}
	if _, exists := r.Rooms[roomId].Peers[id]; exists {
		return true
	}

	return false
}

func (r *RoomRepository) CreatePeer(roomId string, id uint64, canPublish bool, isCaller bool) error {
	r.Lock()

	if !r.doesRoomExists(roomId) {
		room := &Room{
			Mutex:     &sync.Mutex{},
			Peers:     make(map[uint64]*Peer),
			trackLock: &sync.Mutex{},
			Tracks:    make(map[string]*Track),
			timer:     time.NewTicker(3 * time.Second),
		}
		r.Rooms[roomId] = room
		go func() {
			for range room.timer.C {
				room.Lock()
				for _, peer := range room.Peers {
					for _, receiver := range peer.Conn.GetReceivers() {
						if receiver.Track() == nil {
							continue
						}

						go func(peerConn *webrtc.PeerConnection, recv *webrtc.RTPReceiver) {
							err := peerConn.WriteRTCP([]rtcp.Packet{
								&rtcp.PictureLossIndication{
									MediaSSRC: uint32(recv.Track().SSRC()),
								},
							})
							if err != nil {
								println(`[E] [rtcp][PLI] `, err.Error())
							}
						}(peer.Conn, receiver)
					}

				}
				room.Unlock()
			}
		}()
	}

	room := r.Rooms[roomId]
	r.Unlock()

	peerConn, err := r.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: r.conf.ICEServers,
	})
	if err != nil {
		return models.NewError("can't create peer connection", 500, models.MessageResponse{Message: err.Error()})
	}

	peerConn.OnICECandidate(func(ic *webrtc.ICECandidate) {
		r.onPeerICECandidate(roomId, id, ic)
	})
	peerConn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		r.Lock()
		if !r.doesRoomExists(roomId) {
			r.Unlock()
			return
		}
		room := r.Rooms[roomId]
		r.Unlock()
		room.Lock()
		defer room.Unlock()
		peer, stillThere := room.Peers[id]

		r.onPeerConnectionStateChange(room, peer, state)
		{
			if state == webrtc.PeerConnectionStateClosed && isCaller {
				if stillThere && peer.triggeredReconnectOnce {
					return
				}
				go r.onCallerDisconnected(roomId)
			}
		}
	})
	peerConn.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		r.onPeerTrack(roomId, id, remote, receiver)
	})
	/*peerConn.OnNegotiationNeeded(func() {
		println("[PC] negotiating with peer", id)
		r.offerPeer(peerConn,roomId,id)
	})*/
	room.Lock()
	defer room.Unlock()
	room.Peers[id] = &Peer{
		ID:            id,
		Conn:          peerConn,
		HandshakeLock: &sync.Mutex{},
		CanPublish:    canPublish,
		IsCaller:      isCaller,
	}
	go r.updatePCTracks(roomId)
	return nil
}

func (r *RoomRepository) onCallerDisconnected(roomId string) {
	if err := r.ResetRoom(roomId); err != nil {
		println(err.Error())
		return
	}
	*r.conf.StartRejoinCH <- false
}

func (r *RoomRepository) onPeerICECandidate(roomId string, id uint64, ic *webrtc.ICECandidate) {
	if ic == nil {
		return
	}
	reqModel := dto.AddPeerICECandidateReqModel{
		PeerDTO: dto.PeerDTO{
			RoomId: roomId,
			ID:     id,
		},
		ICECandidate: ic.ToJSON(),
	}
	serializedReqBody, err := json.Marshal(reqModel)
	if err != nil {
		println(err.Error())
		return
	}
	resp, err := http.Post(r.conf.LogjamBaseUrl+"/ice", "application/json", bytes.NewReader(serializedReqBody))
	if err != nil {
		println(err.Error())
		return
	}
	if resp.StatusCode > 204 {
		println("POST /ice", resp.StatusCode)
		return
	}
}

func (r *RoomRepository) onPeerConnectionStateChange(room *Room, peer *Peer, newState webrtc.PeerConnectionState) {
	if peer == nil {
		return
	}
	println("[PC] con_stat", newState.String(), peer.ID)
	switch newState {
	case webrtc.PeerConnectionStateDisconnected:
		fallthrough
	case webrtc.PeerConnectionStateFailed:
		if err := peer.Conn.Close(); err != nil {
			println(err.Error())
		}
	case webrtc.PeerConnectionStateClosed:
		delete(room.Peers, peer.ID)
	}
}

func (r *RoomRepository) onPeerTrack(roomId string, id uint64, remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	fmt.Println("got a track!", remote.ID(), remote.StreamID(), remote.Kind().String())
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return
	}
	room := r.Rooms[roomId]
	r.Unlock()

	trackLocal, err := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, remote.ID(), remote.StreamID())
	if err != nil {
		panic(err)
	}
	room.trackLock.Lock()
	room.Tracks[remote.ID()] = &Track{
		OwnerId:    id,
		TrackLocal: trackLocal,
	}
	room.trackLock.Unlock()
	firstVideo := false
	firstAudio := false
	room.Lock()
	peer := room.Peers[id]
	room.Unlock()
	if remote.Kind() == webrtc.RTPCodecTypeVideo && !peer.gotFirstVideoTrack {
		peer.gotFirstVideoTrack = true
		firstVideo = true
	}
	if remote.Kind() == webrtc.RTPCodecTypeAudio && !peer.gotFirstAudioTrack {
		peer.gotFirstAudioTrack = true
		firstAudio = true
	}

	defer func(trackId string) {
		room.trackLock.Lock()
		delete(room.Tracks, trackId)
		room.trackLock.Unlock()
		r.updatePCTracks(roomId)
	}(remote.ID())
	go r.updatePCTracks(roomId)
	buffer := make([]byte, 1500)
	for {
		n, _, err := remote.Read(buffer)
		if err != nil {
			println(err.Error())
			break
		}
		if _, err = trackLocal.Write(buffer[:n]); err != nil {
			println(err.Error())
			break
		}
	}
	if (firstVideo || firstAudio) && peer.IsCaller && !peer.triggeredReconnectOnce {
		go r.onCallerDisconnected(roomId)
		peer.triggeredReconnectOnce = true
	}
}

func (r *RoomRepository) updatePCTracks(roomId string) {
	println("[] updatePCTracks start")
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return
	}
	room := r.Rooms[roomId]
	r.Unlock()
	room.Lock()
	defer room.Unlock()
	for _, peer := range room.Peers {
		if peer.Conn == nil {
			continue
		}
		alreadySentTracks := map[string]*webrtc.RTPSender{}
		receivingPeerTracks := map[string]*webrtc.RTPReceiver{}
		for _, rtpSender := range peer.Conn.GetSenders() {
			if rtpSender.Track() == nil {
				continue
			}
			track := rtpSender.Track()
			alreadySentTracks[track.ID()] = rtpSender
		}
		for _, rtpReceiver := range peer.Conn.GetReceivers() {
			if rtpReceiver.Track() == nil {
				continue
			}
			track := rtpReceiver.Track()
			receivingPeerTracks[track.ID()] = rtpReceiver
		}
		room.trackLock.Lock()
		renegotiate := false
		for id, track := range room.Tracks {
			_, alreadySend := alreadySentTracks[id]
			_, alreadyReceived := receivingPeerTracks[id]
			if track.OwnerId != peer.ID && (!alreadySend && !alreadyReceived) {
				renegotiate = true
				if peer.Conn.ConnectionState() == webrtc.PeerConnectionStateClosed {
					break
				}
				println("[PC] add track", track.TrackLocal.ID(), "to", peer.ID)
				_, err := peer.Conn.AddTrack(track.TrackLocal)
				if err != nil {
					println(err.Error())
					break
				}
			}
		}
		for trackId, rtpSender := range alreadySentTracks {
			if _, exists := room.Tracks[trackId]; !exists {
				renegotiate = true
				if peer.Conn.ConnectionState() == webrtc.PeerConnectionStateClosed {
					break
				}
				println("[PC] remove track", trackId, "from", peer.ID)
				err := peer.Conn.RemoveTrack(rtpSender)
				if err != nil {
					println(err.Error())
					break
				}
			}
		}
		room.trackLock.Unlock()
		if renegotiate {
			go func(p *Peer, rid string) {
				err := r.offerPeer(p, rid)
				if err != nil {
					println(`[E]`, err.Error())
					return
				}
			}(peer, roomId)
		}
	}
	println("[] updatePCTracks end")
}

func (r *RoomRepository) AddPeerIceCandidate(roomId string, id uint64, ic webrtc.ICECandidateInit) error {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return models.NewError("room doesn't exists", 403, map[string]any{"roomId": roomId})
	}
	room := r.Rooms[roomId]
	r.Unlock()
	room.Lock()

	if !r.doesPeerExists(roomId, id) {
		room.Unlock()
		return models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}
	peer := room.Peers[id]
	room.Unlock()

	err := peer.Conn.AddICECandidate(ic)
	if err != nil {
		return models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	return nil
}

func (r *RoomRepository) SetPeerAnswer(roomId string, id uint64, answer webrtc.SessionDescription) error {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return models.NewError("room doesn't exists", 403, map[string]any{"roomId": roomId})
	}
	room := r.Rooms[roomId]
	r.Unlock()
	room.Lock()
	if !r.doesPeerExists(roomId, id) {
		room.Unlock()
		return models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}
	peer := room.Peers[id]
	room.Unlock()
	err := peer.Conn.SetRemoteDescription(answer)
	if err != nil {
		return models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	peer.HandshakeLock.Unlock()
	return nil
}
func (r *RoomRepository) SetPeerOffer(roomId string, id uint64, offer webrtc.SessionDescription) (sdpAnswer *webrtc.SessionDescription, err error) {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return nil, models.NewError("room doesn't exists", 403, map[string]any{"roomId": roomId})
	}
	room := r.Rooms[roomId]
	r.Unlock()
	room.Lock()

	if !r.doesPeerExists(roomId, id) {
		room.Unlock()
		return nil, models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}
	peer := room.Peers[id]
	room.Unlock()

	if !peer.IsCaller {
		return nil, models.NewError("only caller can offer", 403, nil)
	}
	peer.HandshakeLock.Lock()
	defer peer.HandshakeLock.Unlock()
	err = peer.Conn.SetRemoteDescription(offer)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	answer, err := peer.Conn.CreateAnswer(nil)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	err = peer.Conn.SetLocalDescription(answer)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}

	return &answer, nil
}

func (r *RoomRepository) AllowPublish(roomId string, id uint64) error {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return models.NewError("room doesn't exists", 403, map[string]any{"roomId": roomId})
	}
	r.Unlock()
	room := r.Rooms[roomId]
	room.Lock()
	defer room.Unlock()

	if !r.doesPeerExists(roomId, id) {
		return models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}

	room.Peers[id].CanPublish = true
	return nil
}

func (r *RoomRepository) ClosePeer(roomId string, id uint64) error {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return models.NewError("room doesn't exists", 403, map[string]any{"roomId": roomId})
	}
	room := r.Rooms[roomId]
	r.Unlock()
	room.Lock()
	peer := room.Peers[id]

	if !r.doesPeerExists(roomId, id) {
		room.Unlock()
		return models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}
	room.Unlock()
	return peer.Conn.Close()
}

func (r *RoomRepository) ResetRoom(roomId string) error {
	r.Lock()
	defer r.Unlock()
	if !r.doesRoomExists(roomId) {
		return nil
	}
	room := r.Rooms[roomId]
	room.Lock()
	room.timer.Stop()
	for _, peer := range room.Peers {
		go func(conn *webrtc.PeerConnection) {
			_ = conn.Close()
		}(peer.Conn)
	}
	room.Unlock()
	delete(r.Rooms, roomId)
	return nil
}

func (r *RoomRepository) offerPeer(peer *Peer, roomId string) error {
	peer.HandshakeLock.Lock()
	println("[PC] negotiating with peer", peer.ID)
	offer, err := peer.Conn.CreateOffer(nil)
	if err != nil {
		return err
	}
	err = peer.Conn.SetLocalDescription(offer)
	if err != nil {
		return err
	}
	reqModel := dto.SetSDPReqModel{
		PeerDTO: dto.PeerDTO{
			RoomId: roomId,
			ID:     peer.ID,
		},
		SDP: offer,
	}
	bodyJson, err := json.Marshal(reqModel)
	if err != nil {
		return err
	}
	res, err := http.Post(r.conf.LogjamBaseUrl+"/offer", "application/json", bytes.NewReader(bodyJson))
	if err != nil {
		return err
	}
	if res.StatusCode > 204 {
		return errors.New("POST {logjambaseurl}/offer : " + res.Status)
	}
	return nil
}
