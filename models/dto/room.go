package dto

import "github.com/pion/webrtc/v3"

type PeerDTO struct {
	RoomId string `json:"roomId"`
	ID     uint64 `json:"id"`
}

func (model *PeerDTO) Validate() bool {
	return len(model.RoomId) > 0
}

type CreatePeerReqModel struct {
	PeerDTO
	GGID       uint64 `json:"ggid"`
	CanPublish bool   `json:"canPublish"`
	IsCaller   bool   `json:"isCaller"`
}

type AddPeerICECandidateReqModel struct {
	PeerDTO
	GGID         uint64                  `json:"ggid"`
	ICECandidate webrtc.ICECandidateInit `json:"iceCandidate"`
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
