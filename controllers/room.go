package controllers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"sourcecode.social/greatape/goldgorilla/models"
	"sourcecode.social/greatape/goldgorilla/models/dto"
	"sourcecode.social/greatape/goldgorilla/repositories"
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
	err := c.repo.CreatePeer(reqModel.RoomId, reqModel.ID, reqModel.CanPublish, reqModel.IsCaller, reqModel.GGID, reqModel.ConnDirection)
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
	tryCounter := 0
start:
	err := c.repo.AddPeerIceCandidate(reqModel.RoomId, reqModel.ID, reqModel.ICECandidate, reqModel.ConnDirection)
	if err != nil {
		if tryCounter < 4 {
			time.Sleep(1 * time.Second)
			tryCounter++
			goto start
		}
	}

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
	println("[offer] from", reqModel.ID)
	answer, err := c.repo.SetPeerOffer(reqModel.RoomId, reqModel.ID, reqModel.SDP)
	if c.helper.HandleIfErr(ctx, err, nil) {
		println(err.Error())
		return
	}
	c.helper.Response(ctx, struct{}{}, http.StatusNoContent)
	{
		ggid := c.repo.GetRoomGGID(reqModel.RoomId)
		if ggid == nil {
			println("ggid is nil, not answering for offer")
			return
		}
		buffer, err := json.Marshal(dto.SetSDPReqModel{
			PeerDTO: dto.PeerDTO{
				RoomId: reqModel.RoomId,
				ID:     reqModel.ID,
			},
			GGID: *ggid,
			SDP:  *answer,
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
	println("[answer] from", reqModel.ID)
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
	ggid, err := c.repo.ResetRoom(roomId)
	if c.helper.HandleIfErr(ctx, err, nil) {
		return
	}

	c.helper.Response(ctx, struct {
		GGID uint64 `json:"ggid"`
	}{
		GGID: ggid,
	}, http.StatusOK)
}

func (c *RoomController) Start(ctx *gin.Context) {
	reqModel := struct {
		RoomId string `json:"roomId"`
	}{}
	if err := ctx.ShouldBindJSON(&reqModel); err != nil {
		c.helper.ResponseBadReq(ctx)
		return
	}
	buffer, _ := json.Marshal(map[string]any{"roomId": reqModel.RoomId})
	body := bytes.NewReader(buffer)
	res, err := http.Post(c.conf.LogjamBaseUrl+"/join", "application/json", body)
	if err != nil {
		println(err.Error())
		time.Sleep(4 * time.Second)
	}
	if res != nil {
		if res.StatusCode > 204 {
			resbody, _ := io.ReadAll(res.Body)
			println("get /join "+res.Status, string(resbody))
		} else {
			resbody, _ := io.ReadAll(res.Body)
			println(string(resbody))
			respData := struct {
				ID uint64 `json:"id"`
			}{}
			if len(resbody) > 2 {
				err := json.Unmarshal(resbody, &respData)
				if err != nil {
					println(err.Error())
					c.helper.Response(ctx, nil, http.StatusBadRequest)
					return
				}
				if respData.ID > 0 {
					c.repo.CreateRoom(reqModel.RoomId, respData.ID)
				}
			}
		}
	}
	c.helper.Response(ctx, nil, http.StatusNoContent)
}

func (c *RoomController) HealthCheck(ctx *gin.Context) {
	roomId := ctx.Query("roomId")
	if len(roomId) > 0 {
		if !c.repo.DoesRoomExists(roomId) {
			ctx.Status(http.StatusNotFound)
			return
		}
	}
	ctx.Status(204)
}
