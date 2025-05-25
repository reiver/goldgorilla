package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"io"
	"net/http"
	"os"
	"os/signal"
	"github.com/greatape/goldgorilla/controllers"
	"github.com/greatape/goldgorilla/models"
	"github.com/greatape/goldgorilla/repositories"
	"github.com/greatape/goldgorilla/routers"
	"syscall"
	"time"
)

type App struct {
	conf   *models.ConfigModel
	router *routers.Router
	src    string
}

func (a *App) Init(srcListenAddr string, logjamBaseUrl string, iceTCPMUXListenPort uint, customICEHostCandidateIP string) {
	println("initializing ..")
	a.src = srcListenAddr
	var iceServers []webrtc.ICEServer
	iceconfjson, err := os.ReadFile("./ice.servers.json")
	if err != nil {
		println("[E] error reading ice.servers.json: " + err.Error())
	} else {
		err = json.Unmarshal(iceconfjson, &iceServers)
		if err != nil {
			panic("[E] can't parse ice.servers.json: " + err.Error())
		}
	}
	startRejoinCH := make(chan models.RejoinMode, 2)
	a.conf = &models.ConfigModel{
		LogjamBaseUrl:            logjamBaseUrl,
		ICEServers:               iceServers,
		ICETCPMUXListenPort:      iceTCPMUXListenPort,
		CustomICEHostCandidateIP: customICEHostCandidateIP,
		StartRejoinCH:            &startRejoinCH,
	}
	roomRepo := repositories.NewRoomRepository(a.conf)
	a.router = &routers.Router{}
	respHelper := controllers.NewResponseHelper()
	roomCtrl := controllers.NewRoomController(respHelper, roomRepo, a.conf)

	err = a.router.RegisterRoutes(roomCtrl)
	panicIfErr(err)

	{
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGILL)
		go func() {
			a.onDie(<-sigs)
		}()
	}
}

func (a *App) Run() {
	go func() {
		//*a.conf.StartRejoinCH <- true
		c := &http.Client{
			Timeout: 8 * time.Second,
		}
		for data := range *a.conf.StartRejoinCH {
			if data.SimplyJoin {
				buffer, _ := json.Marshal(map[string]any{"roomId": data.RoomId})
				body := bytes.NewReader(buffer)
				res, err := c.Post(a.conf.LogjamBaseUrl+"/join", "application/json", body)
				if err != nil {
					println(err.Error())
					time.Sleep(4 * time.Second)
					*a.conf.StartRejoinCH <- data
					continue
				}
				if res.StatusCode > 204 {
					resbody, _ := io.ReadAll(res.Body)
					println("get /join "+res.Status, string(resbody))
					time.Sleep(4 * time.Second)
					*a.conf.StartRejoinCH <- data
					continue
				}
			} else {
				data.SimplyJoin = true
				reqModel := struct {
					RoomId string `json:"roomId"`
				}{
					RoomId: data.RoomId,
				}
				serializedReqBody, err := json.Marshal(reqModel)
				if err != nil {
					println(err.Error())
					*a.conf.StartRejoinCH <- data
					continue
				}

				resp, err := c.Post(a.conf.LogjamBaseUrl+"/rejoin", "application/json", bytes.NewReader(serializedReqBody))
				if err != nil {
					println(err.Error())
					continue
				}
				if resp.StatusCode > 204 {
					println("/rejoin", resp.Status)
				}
				/*if err != nil {
					println(err.Error())
					*a.conf.StartRejoinCH <- data
					continue
				}
				if resp.StatusCode > 204 {
					println("/rejoin", resp.Status)
					*a.conf.StartRejoinCH <- data
				}*/
			}
		}
	}()
	err := a.router.Serve(a.src)
	panicIfErr(err)
}

func (a *App) onDie(sig os.Signal) {
	fmt.Println("<-", sig)

	os.Exit(0)
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}
