package main

import (
	"bytes"
	"encoding/json"
	"sourcecode.social/greatape/goldgorilla/controllers"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/repositories"
	"sourcecode.social/greatape/goldgorilla/routers"
	"io"
	"net/http"
	"time"
)

type App struct {
	conf   *models.ConfigModel
	router *routers.Router
	src    string
}

func (a *App) Init(srcListenAddr string, logjamBaseUrl string, targetRoom string) {
	a.src = srcListenAddr
	a.conf = &models.ConfigModel{
		LogjamBaseUrl: logjamBaseUrl + "/auxiliary-node",
		TargetRoom:    targetRoom,
	}
	roomRepo := repositories.NewRoomRepository(a.conf)
	a.router = &routers.Router{}
	respHelper := controllers.NewResponseHelper()
	roomCtrl := controllers.NewRoomController(respHelper, roomRepo, a.conf)

	err := a.router.RegisterRoutes(roomCtrl)
	panicIfErr(err)
}

func (a *App) Run() {
	go func() {
	start:
		buffer, _ := json.Marshal(map[string]any{"roomId": a.conf.TargetRoom})
		body := bytes.NewReader(buffer)
		res, err := http.Post(a.conf.LogjamBaseUrl+"/join", "application/json", body)
		if err != nil {
			println(err.Error())
			time.Sleep(4 * time.Second)
			goto start
		}
		if res.StatusCode > 204 {
			resbody, _ := io.ReadAll(res.Body)
			println("get /join "+res.Status, string(resbody))
			time.Sleep(4 * time.Second)
			goto start
		}
	}()
	err := a.router.Serve(a.src)
	panicIfErr(err)
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}
