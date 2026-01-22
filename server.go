package main

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type SubsServer struct {
	Router       *gin.Engine
	Config       *SubsServerConfig
	TokenManager *TokenManager
}

func (s *SubsServer) initRoute() {
	// api url
	s.Router.GET("/api/:apiPath", func(c *gin.Context) {
		apiPath := c.Param("apiPath")
		c.JSON(http.StatusOK, gin.H{"apiPath": apiPath})
	})

	// index
	s.Router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"msg":  "proxy-subs-backend",
			"code": 200,
		})
	})

	// favicon.ico handler
	s.Router.StaticFile("/favicon.ico", "./static/favicon.ico")
}

func (s *SubsServer) init() {
	if s.Config == nil || s.TokenManager == nil {
		panic("SubsConfig or TokenManager is nil")
	}

	s.initRoute()

	var lAddr string = s.Config.ListenHost + ":" + strconv.Itoa(s.Config.ListenPort)
	log.Default().Println("SubsServer init listening on " + lAddr)
	err := s.Router.Run(lAddr)
	if err != nil {
		panic(err)
	}
}

func (s *SubsServer) StartServer() {
	s.init()
}

func NewSubsServer(c *SubsServerConfig, t *TokenManager) *SubsServer {
	server := SubsServer{
		Router:       gin.Default(),
		Config:       c,
		TokenManager: t,
	}

	return &server
}
