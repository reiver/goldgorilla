package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/reiver/goldgorilla/controllers"
)

func registerRoomRoutes(rg *gin.RouterGroup, ctrl *controllers.RoomController) {

	rg.POST("/peer", ctrl.CreatePeer)
	rg.DELETE("/peer", ctrl.ClosePeer)

	rg.POST("/ice", ctrl.AddICECandidate)
	rg.POST("/answer", ctrl.Answer)
	rg.POST("/offer", ctrl.Offer)

	rg.POST("/", ctrl.Start)
	rg.DELETE("/", ctrl.ResetRoom)

}
