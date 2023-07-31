package controllers

import "github.com/gin-gonic/gin"

func HealthCheck(ctx *gin.Context) {
	ctx.Status(204)
}
