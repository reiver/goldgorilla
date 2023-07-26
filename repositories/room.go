package repositories

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/models/dto"
	"log"
	"net/http"
	"sync"
	"time"
)

type Track struct {
	OwnerId    uint64
	TrackLocal *webrtc.TrackLocalStaticRTP
}

type Peer struct {
	ID         uint64
	Conn       *webrtc.PeerConnection
	CanPublish bool
}

type Room struct {
	*sync.Mutex
	Peers     map[uint64]*Peer
	trackLock *sync.Mutex
	Tracks    map[string]*Track
}

type RoomRepository struct {
	Rooms map[string]*Room
	conf  *models.ConfigModel
	*sync.Mutex
}

func NewRoomRepository(conf *models.ConfigModel) *RoomRepository {
	return &RoomRepository{
		Mutex: &sync.Mutex{},
		Rooms: make(map[string]*Room),
		conf:  conf,
	}
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

func (r *RoomRepository) CreatePeer(roomId string, id uint64, canPublish, isCaller bool) (*webrtc.SessionDescription, error) {
	r.Lock()

	if !r.doesRoomExists(roomId) {
		room := &Room{
			Mutex:     &sync.Mutex{},
			Peers:     make(map[uint64]*Peer),
			trackLock: &sync.Mutex{},
			Tracks:    make(map[string]*Track),
		}
		r.Rooms[roomId] = room
		go func() {
			for range time.NewTicker(3 * time.Second).C {
				room.Lock()
				for _, peer := range room.Peers {
					for _, receiver := range peer.Conn.GetReceivers() {
						if receiver.Track() == nil {
							continue
						}

						go peer.Conn.WriteRTCP([]rtcp.Packet{
							&rtcp.PictureLossIndication{
								MediaSSRC: uint32(receiver.Track().SSRC()),
							},
						})
					}

				}
				room.Unlock()
			}
		}()
	}

	r.Unlock()
	room := r.Rooms[roomId]
	room.Lock()
	defer room.Unlock()

	peerConn, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, models.NewError("can't create peer connection", 500, models.MessageResponse{Message: err.Error()})
	}
	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := peerConn.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		}); err != nil {
			return nil, models.NewError("unhandled error, contact support #1313", 500, err)
		}
	}

	peerConn.OnICECandidate(func(ic *webrtc.ICECandidate) {
		r.onPeerICECandidate(roomId, id, ic)
	})
	peerConn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		r.onPeerConnectionStateChange(roomId, id, state)
	})
	peerConn.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		r.onPeerTrack(roomId, id, remote, receiver)
	})

	var sdp *webrtc.SessionDescription
	if !isCaller {
		offer, err := peerConn.CreateOffer(nil)
		if err != nil {
			println(err.Error())
			peerConn.Close()
			return nil, models.NewError("unhandled error, contact support #1314", 500, err)
		}
		err = peerConn.SetLocalDescription(offer)
		if err != nil {
			println(err.Error())
			peerConn.Close()
			return nil, models.NewError("unhandled error, contact support #1315", 500, err)
		}
		sdp = &offer
	}

	room.Peers[id] = &Peer{
		ID:   id,
		Conn: peerConn,
	}

	return sdp, nil
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

func (r *RoomRepository) onPeerConnectionStateChange(roomId string, id uint64, newState webrtc.PeerConnectionState) {
	r.Lock()

	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return
	}
	r.Unlock()
	room := r.Rooms[roomId]
	room.Lock()
	defer room.Unlock()

	switch newState {
	case webrtc.PeerConnectionStateFailed:
		if err := room.Peers[id].Conn.Close(); err != nil {
			log.Print(err)
		}
	case webrtc.PeerConnectionStateClosed:
		delete(room.Peers, id)
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

	defer func() {
		room.trackLock.Lock()
		delete(room.Tracks, remote.ID())
		room.trackLock.Unlock()
		r.updatePCTracks(roomId)
	}()
	go r.updatePCTracks(roomId)
	buffer := make([]byte, 1500)
	for {
		n, _, err := remote.Read(buffer)
		if err != nil {
			println(err.Error())
			return
		}
		if _, err = trackLocal.Write(buffer[:n]); err != nil {
			println(err.Error())
			return
		}
	}
}

func (r *RoomRepository) updatePCTracks(roomId string) {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return
	}
	room := r.Rooms[roomId]
	defer r.Unlock()
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
		for id, track := range room.Tracks {
			_, alreadySend := alreadySentTracks[id]
			_, alreadyReceiver := receivingPeerTracks[id]
			if track.OwnerId != peer.ID && (!alreadySend && !alreadyReceiver) {
				go func(peer *Peer, track *Track) {
					println("add track")
					_, err := peer.Conn.AddTrack(track.TrackLocal)
					if err != nil {
						println(err.Error())
					}
				}(peer, track)
			}
		}
		room.trackLock.Unlock()

		/*room.trackLock.Lock()
		for trackId, _ := range alreadySentTracks {
			if _, exists := room.Tracks[trackId]; !exists {
				go func(peer *Peer, rtpSender *webrtc.RTPSender) {
					err := peer.Conn.RemoveTrack(rtpSender)
					if err != nil {
						println(err.Error())
					}
				}(peer, alreadySentTracks[trackId])
			}
		}
		room.trackLock.Unlock()*/
	}
}

func (r *RoomRepository) AddPeerIceCandidate(roomId string, id uint64, ic webrtc.ICECandidateInit) error {
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

	err := room.Peers[id].Conn.AddICECandidate(ic)
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
	r.Unlock()
	room := r.Rooms[roomId]
	room.Lock()
	defer room.Unlock()

	if !r.doesPeerExists(roomId, id) {
		return models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}

	err := room.Peers[id].Conn.SetRemoteDescription(answer)
	if err != nil {
		return models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	return nil
}
func (r *RoomRepository) SetPeerOffer(roomId string, id uint64, offer webrtc.SessionDescription) (sdpAnswer *webrtc.SessionDescription, err error) {
	r.Lock()
	if !r.doesRoomExists(roomId) {
		r.Unlock()
		return nil, models.NewError("room doesn't exists", 403, map[string]any{"roomId": roomId})
	}
	r.Unlock()
	room := r.Rooms[roomId]
	room.Lock()
	defer room.Unlock()

	if !r.doesPeerExists(roomId, id) {
		return nil, models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}

	err = room.Peers[id].Conn.SetRemoteDescription(offer)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	answer, err := room.Peers[id].Conn.CreateAnswer(nil)
	if err != nil {
		return nil, models.NewError(err.Error(), 500, models.MessageResponse{Message: err.Error()})
	}
	err = room.Peers[id].Conn.SetLocalDescription(answer)
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
	r.Unlock()
	room := r.Rooms[roomId]
	room.Lock()
	defer room.Unlock()

	if !r.doesPeerExists(roomId, id) {
		return models.NewError("no such a peer with this id in this room", 403, map[string]any{"roomId": roomId, "peerId": id})
	}
	return room.Peers[id].Conn.Close()
}
