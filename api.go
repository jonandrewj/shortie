package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type shortieAPI struct {
	storage urlStorage
}

type urlStorage interface {
	SaveURL(ctx context.Context, shortID string, url string, expiration int64) error
	GetURL(ctx context.Context, shortID string) (string, error)
	DeleteURL(ctx context.Context, shortID string) error
	GetStatistics(ctx context.Context, shortID string) (map[string]int64, error)
}

func (api shortieAPI) GetRouter() *gin.Engine {
	router := gin.Default() // Default gives us logging and a recover function built-in

	router.POST("/shortie", api.CreateURL)
	router.GET("/shortie/:id", api.HandleRedirect)
	router.DELETE("/shortie/:id", api.DeleteURL)
	router.GET("/shortie/:id/stats", api.GetUsageStats)
	err := router.SetTrustedProxies(nil)
	if err != nil {
		log.Println("error: " + err.Error())
		panic(err)
	}

	return router
}

func (api shortieAPI) CreateURL(c *gin.Context) {
	var body = struct {
		URL        string `json:"url"`        // TODO: Add validation to this URL
		Expiration int64  `json:"expiration"` // TODO: Add validation to this expiration timestamp
	}{}
	err := c.BindJSON(&body)
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	data := []byte(body.URL)
	guid := uuid.NewSHA1(uuid.NameSpaceURL, data)
	// TODO: handle conflicts - we can check the DB and if we have a conflict give this a couple more characters
	shortID := strings.ReplaceAll(guid.String(), "-", "")[0:10]

	err = api.storage.SaveURL(c, shortID, body.URL, body.Expiration)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, map[string]string{"shortUrl": "http://localhost:8421/shortie/" + shortID})
}

func (api shortieAPI) HandleRedirect(c *gin.Context) {
	shortID := c.Param("id")

	url, err := api.storage.GetURL(c, shortID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if url == "" {
		c.String(http.StatusNotFound, "Not Found")
		return
	}
	c.Header("Location", url)
	c.Status(http.StatusTemporaryRedirect)
}

func (api shortieAPI) DeleteURL(c *gin.Context) {
	shortID := c.Param("id")
	err := api.storage.DeleteURL(c, shortID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func (api shortieAPI) GetUsageStats(c *gin.Context) {
	shortID := c.Param("id")

	usage, err := api.storage.GetStatistics(c, shortID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Usage is stored as a map of UTC day timestamps rounded to the nearest day
	// Days with no usage are not present in the map
	// TODO: make the usage a struct with much more user-friendly names and access points
	todayTimestamp := UTCTimestampOfTodayRounded()
	todayUsage := usage[strconv.Itoa(int(UTCTimestampOfTodayRounded().Unix()))]

	weekUsage := todayUsage
	dayTimestamp := todayTimestamp
	for i := 0; i < 6; i++ {
		dayTimestamp = dayTimestamp.Add(-time.Hour * 24)
		weekUsage += usage[strconv.Itoa(int(dayTimestamp.Unix()))]
	}

	totalUsage := int64(0)
	for _, dayUsage := range usage {
		totalUsage += dayUsage
	}

	c.JSON(http.StatusOK, map[string]int64{
		"lastDay":  todayUsage,
		"lastWeek": weekUsage,
		"allTime":  totalUsage,
	})
}
