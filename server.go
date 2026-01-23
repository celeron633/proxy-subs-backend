package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type SubsServer struct {
	Router       *gin.Engine
	Config       *SubsServerConfig
	TokenManager *TokenManager
	ApiSwitch    *ApiSwitch
}

// subscribe API logic handler
func (s *SubsServer) apiHandler(c *gin.Context) {
	if !s.ApiSwitch.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "503", "message": "api switch is disabled"})
		return
	}

	apiPath := c.Param("apiPath")
	token := c.Query("token")

	log.Default().Printf("apiHandler path:[%s], token:[%s]", apiPath, token)
	if len(apiPath) == 0 || len(token) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "400", "msg": "param err"})
		return
	}

	// validate token
	if !s.TokenManager.ValidateToken(token) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "400", "msg": "invalid token"})
		return
	}

	// download subscribe file
	subsConfig := s.findSubsConfig(apiPath)
	if subsConfig == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "400", "msg": fmt.Sprintf("subs not found matched tag:[%s]", apiPath)})
		return
	}

	// check if file exist
	_, err := os.Stat(subsConfig.FilePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "400", "msg": fmt.Sprintf("subs config file [%s] for TAG [%s] not exists!", subsConfig.FilePath, subsConfig.Tag)})
		return
	}

	// download file
	c.FileAttachment(subsConfig.FilePath, subsConfig.Tag)
}

// findSubsConfig finds the matching subscription config by apiPath
func (s *SubsServer) findSubsConfig(apiPath string) *SubsConfig {
	for _, config := range s.Config.SubsConfigs {
		if strings.Contains(config.Tag, apiPath) {
			log.Default().Printf("apiHandler apiPath:[%s] matched TAG:[%s]", apiPath, config.Tag)
			return &config
		}
	}
	return nil
}

func (s *SubsServer) initRoute() {
	// api url
	s.Router.GET("/api/:apiPath", s.apiHandler)

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

	lAddr := s.Config.ListenHost + ":" + strconv.Itoa(s.Config.ListenPort)
	log.Default().Println("SubsServer init listening on " + lAddr)
	err := s.Router.Run(lAddr)
	if err != nil {
		panic(err)
	}
}

func (s *SubsServer) StartServer() {
	s.init()
}

func NewSubsServer(c *SubsServerConfig, t *TokenManager, a *ApiSwitch) *SubsServer {
	server := SubsServer{
		Router:       gin.Default(),
		Config:       c,
		TokenManager: t,
		ApiSwitch:    a,
	}

	return &server
}
