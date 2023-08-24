package models

import "github.com/pion/webrtc/v3"

type ConfigModel struct {
	ServiceAddress string             `json:"serviceAddress"`
	LogjamBaseUrl  string             `json:"logjamBaseUrl"`
	TargetRoom     string             `json:"targetRoom"`
	ICEServers     []webrtc.ICEServer `json:"iceServers"`
}
