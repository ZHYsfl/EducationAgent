package server

import "github.com/gin-gonic/gin"

func SetupRouter(app *App) *gin.Engine {
	r := gin.Default()

	// Local OSS object serving (used by local storage signed URLs).
	r.GET("/oss/*object_key", app.serveOSSObject)

	v1 := r.Group("/api/v1")
	{
		files := v1.Group("/files")
		files.Use(AuthUserMiddleware())
		{
			files.POST("/upload", app.uploadFile)
			files.GET("/:file_id", app.getFile)
			files.DELETE("/:file_id", app.deleteFile)
		}

		sessions := v1.Group("/sessions")
		sessions.Use(AuthUserMiddleware())
		{
			sessions.POST("", app.createSession)
			sessions.GET("/:session_id", app.getSession)
			sessions.GET("", app.listSessions)
			sessions.PUT("/:session_id", app.updateSession)
		}

		search := v1.Group("/search")
		search.Use(AuthUserMiddleware())
		{
			search.POST("/query", app.searchQuery)
			search.GET("/results/:request_id", app.searchResult)
		}
	}

	return r
}
