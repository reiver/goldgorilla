package routers

import (
	"github.com/gin-gonic/gin"
	"sourcecode.social/greatape/goldgorilla/controllers"
)

type Router struct {
	router *gin.Engine
}

func (r *Router) RegisterRoutes(rCtrl *controllers.RoomController) error {
	gin.SetMode(gin.ReleaseMode)
	r.router = gin.Default()
	registerRoomRoutes(r.router.Group("/room"), rCtrl)
	r.router.GET("/healthcheck", controllers.HealthCheck)

	return nil
}

func (r *Router) Serve(addr string) error {
	println("serving on ", addr)
	return r.router.Run(addr)
}
