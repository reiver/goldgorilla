package controllers

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/models/dto"
	"sourcecode.social/greatape/goldgorilla/repositories"
	"time"
)

type RoomController struct {
	helper *ResponseHelper
	repo   *repositories.RoomRepository
	conf   *models.ConfigModel
}

func NewRoomController(respHelper *ResponseHelper, repo *repositories.RoomRepository, conf *models.ConfigModel) *RoomController {
	return &RoomController{
		helper: respHelper,
		repo:   repo,
		conf:   conf,
	}
}

func (c *RoomController) CreatePeer(ctx *gin.Context) {
	var reqModel dto.CreatePeerReqModel
	badReqSt := 400
	if err := ctx.ShouldBindJSON(&reqModel); c.helper.HandleIfErr(ctx, err, &badReqSt) {
		return
	}
	if !reqModel.Validate() {
		c.helper.ResponseUnprocessableEntity(ctx)
		return
	}
	err := c.repo.CreatePeer(reqModel.RoomId, reqModel.ID, reqModel.CanPublish, reqModel.IsCaller)
	if c.helper.HandleIfErr(ctx, err, nil) {
		return
	}
	c.helper.Response(ctx, struct{}{}, http.StatusNoContent)
}

func (c *RoomController) AddICECandidate(ctx *gin.Context) {
	var reqModel dto.AddPeerICECandidateReqModel
	badReqSt := 400
	if err := ctx.ShouldBindJSON(&reqModel); c.helper.HandleIfErr(ctx, err, &badReqSt) {
		return
	}
	if !reqModel.Validate() {
		c.helper.ResponseUnprocessableEntity(ctx)
		return
	}
	err := c.repo.AddPeerIceCandidate(reqModel.RoomId, reqModel.ID, reqModel.ICECandidate)
	if c.helper.HandleIfErr(ctx, err, nil) {
		return
	}
	c.helper.Response(ctx, struct{}{}, http.StatusNoContent)
}

func (c *RoomController) Offer(ctx *gin.Context) {
	var reqModel dto.SetSDPReqModel
	badReqSt := 400
	if err := ctx.ShouldBindJSON(&reqModel); c.helper.HandleIfErr(ctx, err, &badReqSt) {
		return
	}
	if !reqModel.Validate() {
		c.helper.ResponseUnprocessableEntity(ctx)
		return
	}
	answer, err := c.repo.SetPeerOffer(reqModel.RoomId, reqModel.ID, reqModel.SDP)
	if c.helper.HandleIfErr(ctx, err, nil) {
		println(err.Error())
		return
	}
	c.helper.Response(ctx, struct{}{}, http.StatusNoContent)
	{
		buffer, err := json.Marshal(dto.SetSDPReqModel{
			PeerDTO: dto.PeerDTO{
				RoomId: reqModel.RoomId,
				ID:     reqModel.ID,
			},
			SDP: *answer,
		})
		if err != nil {
			println(err.Error())
			return
		}
		reader := bytes.NewReader(buffer)
		resp, err := http.Post(c.conf.LogjamBaseUrl+"/answer", "application/json", reader)
		if err != nil {
			println(err.Error())
			return
		}
		if resp.StatusCode > 204 {
			println(resp.Status)
		}
	}
}

func (c *RoomController) Answer(ctx *gin.Context) {
	var reqModel dto.SetSDPReqModel
	badReqSt := 400
	if err := ctx.ShouldBindJSON(&reqModel); c.helper.HandleIfErr(ctx, err, &badReqSt) {
		return
	}
	if !reqModel.Validate() {
		c.helper.ResponseUnprocessableEntity(ctx)
		return
	}
	err := c.repo.SetPeerAnswer(reqModel.RoomId, reqModel.ID, reqModel.SDP)
	if c.helper.HandleIfErr(ctx, err, nil) {
		println(err.Error())
		return
	}
	c.helper.Response(ctx, struct{}{}, http.StatusNoContent)
}

func (c *RoomController) ClosePeer(ctx *gin.Context) {
	var reqModel dto.PeerDTO
	badReqSt := 400
	if err := ctx.ShouldBindJSON(&reqModel); c.helper.HandleIfErr(ctx, err, &badReqSt) {
		return
	}
	if !reqModel.Validate() {
		c.helper.ResponseUnprocessableEntity(ctx)
		return
	}
	err := c.repo.ClosePeer(reqModel.RoomId, reqModel.ID)
	if c.helper.HandleIfErr(ctx, err, nil) {
		return
	}
	c.helper.Response(ctx, struct{}{}, http.StatusNoContent)
}

func (c *RoomController) ResetRoom(ctx *gin.Context) {
	var reqModel map[string]any
	badReqSt := 400
	if err := ctx.ShouldBindJSON(&reqModel); c.helper.HandleIfErr(ctx, err, &badReqSt) {
		return
	}
	roomId := ""
	if rid, exists := reqModel["roomId"]; !exists {
		c.helper.ResponseUnprocessableEntity(ctx)
		return
	} else {
		if castedrid, stringItIs := rid.(string); stringItIs {
			roomId = castedrid
		} else {
			c.helper.ResponseUnprocessableEntity(ctx)
			return
		}
	}
	err := c.repo.ResetRoom(roomId)
	if c.helper.HandleIfErr(ctx, err, nil) {
		return
	}

	c.helper.Response(ctx, nil, http.StatusNoContent)
}

func (c *RoomController) Start(ctx *gin.Context) {
	buffer, _ := json.Marshal(map[string]any{"roomId": c.conf.TargetRoom, "svcAddr": c.conf.ServiceAddress})
	body := bytes.NewReader(buffer)
	res, err := http.Post(c.conf.LogjamBaseUrl+"/join", "application/json", body)
	if err != nil {
		println(err.Error())
		time.Sleep(4 * time.Second)
	}
	if res.StatusCode > 204 {
		resbody, _ := io.ReadAll(res.Body)
		println("get /join "+res.Status, string(resbody))
	}
}
