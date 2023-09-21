package models

import "github.com/pion/webrtc/v3"

type ConfigModel struct {
	ServiceAddress           string             `json:"serviceAddress"`
	LogjamBaseUrl            string             `json:"logjamBaseUrl"`
	TargetRoom               string             `json:"targetRoom"`
	ICETCPMUXListenPort      uint               `json:"ice_tcpmux_listenPort"`
	CustomICEHostCandidateIP string             `json:"customICEHostCandidateIP"`
	ICEServers               []webrtc.ICEServer `json:"iceServers"`
}
