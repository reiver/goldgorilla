package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/reiver/goldgorilla/controllers"
)

type Router struct {
	router *gin.Engine
}

func (r *Router) RegisterRoutes(rCtrl *controllers.RoomController) error {
	gin.SetMode(gin.ReleaseMode)
	r.router = gin.Default()
	r.router.Use(gin.Recovery())
	registerRoomRoutes(r.router.Group("/room"), rCtrl)
	r.router.GET("/healthcheck", rCtrl.HealthCheck)

	return nil
}

func (r *Router) Serve(addr string) error {
	println("serving on ", addr)
	return r.router.Run(addr)
}
