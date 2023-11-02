package models

import "github.com/pion/webrtc/v3"

type RejoinMode struct {
	SimplyJoin bool
	RoomId     string
}
type ConfigModel struct {
	LogjamBaseUrl            string             `json:"logjamBaseUrl"`
	ICETCPMUXListenPort      uint               `json:"ice_tcpmux_listenPort"`
	CustomICEHostCandidateIP string             `json:"customICEHostCandidateIP"`
	ICEServers               []webrtc.ICEServer `json:"iceServers"`
	StartRejoinCH            *chan RejoinMode
}
