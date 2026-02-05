package controllers

import (
	"github.com/gin-gonic/gin"
	"codeberg.org/goldgorilla/logjam/models"
)

type ResponseHelper struct {
}

func NewResponseHelper() *ResponseHelper {
	return &ResponseHelper{}
}

func (r *ResponseHelper) Response(ctx *gin.Context, body any, code int) {
	if body == nil {
		body = struct{}{}
	}
	ctx.JSON(code, body)
}

func (r *ResponseHelper) ResponseError(ctx *gin.Context, errObj any, code *int) {
	errCode := 500
	if code != nil {
		errCode = *code
	} else if bigErr, isBigErr := errObj.(models.BigError); isBigErr {
		errCode = bigErr.ErrCode()
	}

	var errResp models.MessageResponse
	if err, isError := errObj.(error); isError {
		errResp.Message = err.Error()
	} else if err, isMessage := errObj.(models.MessageResponse); isMessage {
		errResp = err
	} else {
		errResp.Message = "unhandled error, contact support"
	}
	ctx.JSON(errCode, errResp)
}

func (r *ResponseHelper) ResponseBadReq(ctx *gin.Context) {
	st := 400
	r.ResponseError(ctx, models.MessageResponse{Message: "bad request, invalid input"}, &st)
}

func (r *ResponseHelper) ResponseUnprocessableEntity(ctx *gin.Context) {
	st := 422
	r.ResponseError(ctx, models.MessageResponse{Message: "unprocessable entity, invalid input"}, &st)
}

func (r *ResponseHelper) HandleIfErr(ctx *gin.Context, err error, status *int) bool {
	if err == nil {
		return false
	}
	var statusCode = 500
	if bigErr, extends := err.(models.BigError); extends {
		if status != nil {
			statusCode = *status
		} else {
			statusCode = bigErr.ErrCode()
		}
		r.ResponseError(ctx, models.MessageResponse{Message: bigErr.Error()}, &statusCode)
	} else {
		if status != nil {
			statusCode = *status
		}
		r.ResponseError(ctx, models.MessageResponse{Message: err.Error()}, &statusCode)
	}
	return true
}
