package dto

import "github.com/pion/webrtc/v3"

// not the rtp direction like rtpsender or rtpreceiver; but the connection direction wether conn wants to send or recv
type ConnectionDirection string

const (
	CDSend = "sendOnly"
	CDRecv = "recvOnly"
)

type PeerDTO struct {
	RoomId string `json:"roomId"`
	ID     uint64 `json:"id"`
}

func (model *PeerDTO) Validate() bool {
	return len(model.RoomId) > 0
}

type CreatePeerReqModel struct {
	PeerDTO
	GGID          uint64              `json:"ggid"`
	CanPublish    bool                `json:"canPublish"`
	IsCaller      bool                `json:"isCaller"`
	ConnDirection ConnectionDirection `json:"connectionDirection"`
}

type AddPeerICECandidateReqModel struct {
	PeerDTO
	GGID          uint64                  `json:"ggid"`
	ICECandidate  webrtc.ICECandidateInit `json:"iceCandidate"`
	ConnDirection ConnectionDirection     `json:"connectionDirection"`
}

func (model *AddPeerICECandidateReqModel) Validate() bool {
	return model.PeerDTO.Validate() // && len(model.ICECandidate.Candidate) > 0
}

type SetSDPReqModel struct {
	PeerDTO
	GGID uint64                    `json:"ggid"`
	SDP  webrtc.SessionDescription `json:"sdp"`
}

func (model *SetSDPReqModel) Validate() bool {
	return model.PeerDTO.Validate() && len(model.SDP.SDP) > 0
}
