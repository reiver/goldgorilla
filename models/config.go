package models

type ConfigModel struct {
	ServiceAddress string `json:"serviceAddress"`
	LogjamBaseUrl  string `json:"logjamBaseUrl"`
	TargetRoom     string `json:"targetRoom"`
}
