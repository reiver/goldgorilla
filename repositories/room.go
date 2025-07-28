package repositories

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/models/dto"
)

type Track struct {
	OwnerId    uint64
	TrackLocal *webrtc.TrackLocalStaticRTP
}

type Peer struct {
	ID                     uint64
	RecvConn               *webrtc.PeerConnection
	SendConn               *webrtc.PeerConnection
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
	ggId      uint64
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

func (r *RoomRepository) CreateRoom(roomId string, ggid uint64) {
	r.Lock()
	defer r.Unlock()

	if !r.doesRoomExists(roomId) {
		room := &Room{
			Mutex:     &sync.Mutex{},
			Peers:     make(map[uint64]*Peer),
			trackLock: &sync.Mutex{},
			Tracks:    make(map[string]*Track),
			timer:     time.NewTicker(3 * time.Second),
			ggId:      ggid,
		}
		r.Rooms[roomId] = room
		go func() {
			for range room.timer.C {
				room.Lock()
				for _, peer := range room.Peers {
					if peer.SendConn == nil {
						continue
					}
					for _, receiver := range peer.SendConn.GetReceivers() {
						if receiver.Track() == nil {
							continue
						}
						go func(peerConn *webrtc.PeerConnection, recv *webrtc.RTPReceiver) {
							// err := peerConn.WriteRTCP([]rtcp.Packet{
							_, err := recv.Transport().WriteRTCP([]rtcp.Packet{
								&rtcp.PictureLossIndication{
									MediaSSRC: uint32(recv.Track().SSRC()),
								},
							})
							if err != nil {
								println(`[E] [rtcp][PLI] `, err.Error())
							}
						}(peer.RecvConn, receiver)
					}
				}
				room.Unlock()
			}
		}()
	}
}

func (r *RoomRepository) CreatePeer(roomId string, id uint64, canPublish bool, isCaller bool, ggid uint64, connDirection dto.ConnectionDirection) error {
	r.Lock()

	room := r.Rooms[roomId]
	r.Unlock()

	peerConn, err := r.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: r.conf.ICEServers,
	})
	if err != nil {
		return models.NewError("can't create peer connection", 500, models.MessageResponse{Message: err.Error()})
	}

	peerConn.OnICECandidate(func(ic *webrtc.ICECandidate) {
		r.onPeerICECandidate(roomId, id, room.ggId, ic, connDirection)
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

		r.onPeerConnectionStateChange(room, peer, state, connDirection)
		{
			if state == webrtc.PeerConnectionStateClosed && isCaller {
				if stillThere && peer.triggeredReconnectOnce {
					return
				}
				go r.onCallerDisconnected(roomId)
			}
		}
	})

	if connDirection == dto.CDSend {
		var audioReady, videoReady atomic.Bool
		peerConn.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			switch remote.Kind() {
			case webrtc.RTPCodecTypeAudio:
				audioReady.Store(true)
			case webrtc.RTPCodecTypeVideo:
				videoReady.Store(true)
			}

			if audioReady.Load() && videoReady.Load() {
				r.onPeerTrack(roomId, id, remote, receiver, false)
			} else {
				r.onPeerTrack(roomId, id, remote, receiver, true)
			}
		})
	}
	/*peerConn.OnNegotiationNeeded(func() {
		println("[PC] negotiating with peer", id)
		r.offerPeer(peerConn,roomId,id)
	})*/
	room.Lock()
	defer room.Unlock()
	if _, exists := room.Peers[id]; !exists {
		room.Peers[id] = &Peer{
			ID: id,
			// Conn:          peerConn,
			HandshakeLock: &sync.Mutex{},
			CanPublish:    canPublish,
			IsCaller:      isCaller,
		}
	}
	if connDirection == dto.CDSend {
		room.Peers[id].SendConn = peerConn
	} else {
		room.Peers[id].RecvConn = peerConn
	}

	go r.updatePCTracks(roomId)
	return nil
}

func (r *RoomRepository) onCallerDisconnected(roomId string) {
	/*if _, err := r.ResetRoom(roomId); err != nil {
		println(err.Error())
		return
	}
	*r.conf.StartRejoinCH <- models.RejoinMode{
		SimplyJoin: false,
		RoomId:     roomId,
	}*/
	//not doing it for now
}

func (r *RoomRepository) onPeerICECandidate(roomId string, id, ggid uint64, ic *webrtc.ICECandidate, connDirection dto.ConnectionDirection) {
	if ic == nil {
		return
	}
	reqModel := dto.AddPeerICECandidateReqModel{
		PeerDTO: dto.PeerDTO{
			RoomId: roomId,
			ID:     id,
		},
		GGID:          ggid,
		ICECandidate:  ic.ToJSON(),
		ConnDirection: connDirection,
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

func (r *RoomRepository) onPeerConnectionStateChange(room *Room, peer *Peer, newState webrtc.PeerConnectionState, connDirection dto.ConnectionDirection) {
	if peer == nil {
		return
	}
	println("[PC] con_stat", newState.String(), peer.ID)
	switch newState {
	case webrtc.PeerConnectionStateDisconnected:
		fallthrough
	case webrtc.PeerConnectionStateFailed:
		if connDirection == dto.CDSend {
			if err := peer.SendConn.Close(); err != nil {
				println(err.Error())
			}
		} else {
			if err := peer.RecvConn.Close(); err != nil {
				println(err.Error())
			}
		}
	case webrtc.PeerConnectionStateClosed:
		delete(room.Peers, peer.ID)
	}
}

func (r *RoomRepository) onPeerTrack(roomId string, id uint64, remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver, dontShakeThatHandYet bool) {
	fmt.Println("[in] got a track!", id, remote.ID(), remote.StreamID(), remote.Kind().String())
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
	if !dontShakeThatHandYet {
		go r.updatePCTracks(roomId)
	}
	buffer := make([]byte, 1500)
	for {
		remote.SetReadDeadline(time.Now().Add(8 * time.Second))
		n, _, err := remote.Read(buffer)
		if err != nil {
			println(1, remote.ID(), err.Error())
			break
		}
		if _, err = trackLocal.Write(buffer[:n]); err != nil {
			println(2, remote.ID(), err.Error())
			break
		}
	}
	if (firstVideo || firstAudio) && peer.IsCaller && !peer.triggeredReconnectOnce {
		go r.onCallerDisconnected(roomId)
		peer.triggeredReconnectOnce = true
	}
}

func (r *RoomRepository) updatePCTracks(roomId string) {
	println("[updatePCTracks] start")
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
		// accessing peer.RecvConn is like if peer is reciving then we have rtp senders and accessing peer.SendConn is like if peer is sending then we have rtp receivers.
		if peer.RecvConn == nil {
			continue
		}
		alreadySentTracks := map[string]*webrtc.RTPSender{}
		receivingPeerTracks := map[string]*webrtc.RTPReceiver{}
		if peer.RecvConn != nil {
			for _, rtpSender := range peer.RecvConn.GetSenders() {
				if rtpSender.Track() == nil {
					continue
				}
				track := rtpSender.Track()
				alreadySentTracks[track.ID()] = rtpSender
			}
		}
		if peer.SendConn != nil {
			for _, rtpReceiver := range peer.SendConn.GetReceivers() {
				if rtpReceiver.Track() == nil {
					continue
				}
				track := rtpReceiver.Track()
				receivingPeerTracks[track.ID()] = rtpReceiver
			}
		}
		room.trackLock.Lock()
		renegotiate := false
		for id, track := range room.Tracks {
			_, alreadySend := alreadySentTracks[id]
			_, alreadyReceived := receivingPeerTracks[id]
			if track.OwnerId != peer.ID && (!alreadySend && !alreadyReceived) {
				renegotiate = true
				if peer.RecvConn != nil && peer.RecvConn.ConnectionState() == webrtc.PeerConnectionStateClosed {
					break
				}
				println("[out] add track", track.TrackLocal.ID(), "to", peer.ID)
				_, err := peer.RecvConn.AddTrack(track.TrackLocal)
				/*_, err := peer.Conn.AddTransceiverFromTrack(track.TrackLocal, webrtc.RTPTransceiverInit{
					Direction: webrtc.RTPTransceiverDirectionSendrecv,
				})*/
				if err != nil {
					println(err.Error())
					break
				}
			}
		}
		for trackId, rtpSender := range alreadySentTracks {
			if _, exists := room.Tracks[trackId]; !exists {
				renegotiate = true
				if peer.RecvConn.ConnectionState() == webrtc.PeerConnectionStateClosed {
					break
				}
				println("[PC] remove track", trackId, "from", peer.ID)
				err := peer.RecvConn.RemoveTrack(rtpSender)
				if err != nil {
					println(err.Error())
					break
				}
			}
		}
		room.trackLock.Unlock()
		if renegotiate {
			go func(p *Peer, rid string) {

				err := r.offerPeer(p, rid, dto.CDRecv)
				if err != nil {
					println(`[E]`, err.Error())
					return
				}
			}(peer, roomId)
		}
	}
	println("[updatePCTracks] end")
}

func (r *RoomRepository) AddPeerIceCandidate(roomId string, id uint64, ic webrtc.ICECandidateInit, connDirection dto.ConnectionDirection) error {
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

	var err error
	if connDirection == dto.CDSend {
		err = peer.SendConn.AddICECandidate(ic)
	} else {
		err = peer.RecvConn.AddICECandidate(ic)
	}
	if err != nil {
		return models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	return nil
}

func (r *RoomRepository) SetPeerAnswer(roomId string, id uint64, answer webrtc.SessionDescription, connDirection dto.ConnectionDirection) error {
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
	var err error
	if connDirection == dto.CDSend {
		err = peer.SendConn.SetRemoteDescription(answer)
	} else {
		err = peer.RecvConn.SetRemoteDescription(answer)
	}
	if err != nil {
		return models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	peer.HandshakeLock.Unlock()
	// println("[lock_answer] unlocked handshake for peer:", peer.ID)
	return nil
}
func (r *RoomRepository) SetPeerOffer(roomId string, id uint64, offer webrtc.SessionDescription, connDirection dto.ConnectionDirection) (sdpAnswer *webrtc.SessionDescription, err error) {
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
		// return nil, models.NewError("only caller can offer", 403, nil)
	}
	// println("[lock_offer] locking handshake for peer:", peer.ID)
	peer.HandshakeLock.Lock()
	// println("[lock_offer] locked handshake for peer:", peer.ID)
	defer func() {
		peer.HandshakeLock.Unlock()
		// println("[lock_offer] unlocked handshake for peer:", peer.ID)
	}()
	var targetConn *webrtc.PeerConnection
	if connDirection == dto.CDSend {
		targetConn = peer.SendConn
	} else {
		targetConn = peer.RecvConn
	}
	// defer peer.HandshakeLock.Unlock()
	err = targetConn.SetRemoteDescription(offer)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	answer, err := targetConn.CreateAnswer(nil)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	err = targetConn.SetLocalDescription(answer)
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
	err1 := peer.SendConn.Close()
	err2 := peer.RecvConn.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (r *RoomRepository) ResetRoom(roomId string) (uint64, error) {
	r.Lock()
	defer r.Unlock()
	if !r.doesRoomExists(roomId) {
		return 0, nil
	}
	room := r.Rooms[roomId]
	room.Lock()
	ggid := room.ggId
	room.timer.Stop()
	for _, peer := range room.Peers {
		go func(conns ...*webrtc.PeerConnection) {
			for _, c := range conns {
				if c != nil {
					_ = c.Close()
				}
			}
		}(peer.RecvConn, peer.SendConn)
	}
	room.Unlock()
	delete(r.Rooms, roomId)
	return ggid, nil
}

func (r *RoomRepository) offerPeer(peer *Peer, roomId string, connDirection dto.ConnectionDirection) error {
	// println("[lock_op] locking handshake for peer:", peer.ID)
	peer.HandshakeLock.Lock()
	println("[PC] negotiating with peer", peer.ID)
	var targetConn *webrtc.PeerConnection
	if connDirection == dto.CDSend {
		targetConn = peer.SendConn
	} else {
		targetConn = peer.RecvConn
	}
	offer, err := targetConn.CreateOffer(nil)
	if err != nil {
		return err
	}
	err = targetConn.SetLocalDescription(offer)
	if err != nil {
		return err
	}
	ggid := r.GetRoomGGID(roomId)
	if ggid == nil {
		return errors.New("room doesnt have a ggid ( meeting is done or not started yet )")
	}
	reqModel := dto.SetSDPReqModel{
		GGID: *ggid,
		PeerDTO: dto.PeerDTO{
			RoomId: roomId,
			ID:     peer.ID,
		},
		SDP:           offer,
		ConnDirection: connDirection,
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

func (r *RoomRepository) GetRoomGGID(roomId string) *uint64 {
	r.Lock()
	defer r.Unlock()
	if !r.doesRoomExists(roomId) {
		return nil
	}
	gid := r.Rooms[roomId].ggId
	return &gid
}
