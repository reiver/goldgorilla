package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sourcecode.social/greatape/goldgorilla/controllers"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/repositories"
	"sourcecode.social/greatape/goldgorilla/routers"
	"syscall"
	"time"
)

type App struct {
	conf   *models.ConfigModel
	router *routers.Router
	src    string
}

func (a *App) Init(srcListenAddr string, svcAddr string, logjamBaseUrl string, targetRoom string) {
	println("initializing ..")
	a.src = srcListenAddr
	a.conf = &models.ConfigModel{
		LogjamBaseUrl:  logjamBaseUrl + "/auxiliary-node",
		TargetRoom:     targetRoom,
		ServiceAddress: svcAddr,
	}
	roomRepo := repositories.NewRoomRepository(a.conf)
	a.router = &routers.Router{}
	respHelper := controllers.NewResponseHelper()
	roomCtrl := controllers.NewRoomController(respHelper, roomRepo, a.conf)

	err := a.router.RegisterRoutes(roomCtrl)
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
	start:
		buffer, _ := json.Marshal(map[string]any{"roomId": a.conf.TargetRoom, "svcAddr": a.conf.ServiceAddress})
		body := bytes.NewReader(buffer)
		c := &http.Client{
			Timeout: 8 * time.Second,
		}
		res, err := c.Post(a.conf.LogjamBaseUrl+"/join", "application/json", body)
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

func (a *App) onDie(sig os.Signal) {
	fmt.Println("<-", sig)

	os.Exit(0)
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}
