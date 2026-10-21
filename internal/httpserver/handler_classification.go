package httpserver

import (
	"net/http"
	"strconv"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/gin-gonic/gin"
)

type ClassificationHandler struct {
	dao      *dao.Query
	blocking blocking.Manager
}

func NewClassificationHandler(q lazy.Lazy[*dao.Query], bm lazy.Lazy[blocking.Manager]) (*ClassificationHandler, error) {
	dao, err := q.Get()
	if err != nil {
		return nil, err
	}
	manager, err := bm.Get()
	if err != nil {
		return nil, err
	}
	return &ClassificationHandler{dao: dao, blocking: manager}, nil
}

type PatchClassificationItem struct {
	InfoHash string   `json:"info_hash"`
	Action   string   `json:"action"` // "do_nothing" or "delete"
	Tags     []string `json:"tags"`
	// ContentType, when present, replaces the torrent's content type.
	// Must be a valid content type value (e.g. "movie", "music", "xxx");
	// invalid values are ignored. This lets an external classifier
	// override the builtin classification after the fact.
	ContentType *string `json:"content_type"`
}

func (h *ClassificationHandler) Key() string {
	return "classification"
}

func (h *ClassificationHandler) Apply(e *gin.Engine) error {
	h.RegisterRoutes(e)
	return nil
}

func (h *ClassificationHandler) RegisterRoutes(e *gin.Engine) {
	group := e.Group("/api/v1/classification")
	{
		group.GET("", h.GetBatch)
		group.PATCH("", h.PatchBatch)
	}
}

func (h *ClassificationHandler) GetBatch(c *gin.Context) {
	tag := c.Query("tag")
	excludeTag := c.Query("exclude_tag")
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}

	db := h.dao.TorrentContent.WithContext(c.Request.Context()).ReadDB().UnderlyingDB()

	if tag != "" {
		tagSubQuery := h.dao.TorrentTag.WithContext(c.Request.Context()).Select(h.dao.TorrentTag.InfoHash).Where(h.dao.TorrentTag.Name.Eq(tag))
		db = db.Where("info_hash IN (?)", tagSubQuery)
	}

	if excludeTag != "" {
		excludeSubQuery := h.dao.TorrentTag.WithContext(c.Request.Context()).Select(h.dao.TorrentTag.InfoHash).Where(h.dao.TorrentTag.Name.Eq(excludeTag))
		db = db.Where("info_hash NOT IN (?)", excludeSubQuery)
	}

	var results []model.TorrentContent
	err := db.Limit(limit).Find(&results).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": results,
	})
}

func (h *ClassificationHandler) PatchBatch(c *gin.Context) {
	var items []PatchClassificationItem
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Deletes go through the blocking manager (same as the UI delete):
	// the rows are deleted and the hashes tombstoned in the blocked
	// torrents bloom filter, so the crawler never re-discovers them.
	var toDelete []protocol.ID

	for _, item := range items {
		infoHash, err := protocol.ParseID(item.InfoHash)
		if err != nil {
			continue
		}
		if item.Action == "delete" {
			toDelete = append(toDelete, infoHash)
			continue
		}
		if item.ContentType != nil {
			if ct, parseErr := model.ParseContentType(*item.ContentType); parseErr == nil {
				db := h.dao.TorrentContent.WithContext(c.Request.Context()).UnderlyingDB()
				_ = db.Model(&model.TorrentContent{}).
					Where("info_hash = ?", infoHash).
					Update("content_type", model.NewNullContentType(ct)).Error
			}
		}
		if item.Tags != nil {
			_, _ = h.dao.TorrentTag.WithContext(c.Request.Context()).Where(h.dao.TorrentTag.InfoHash.Eq(infoHash)).Delete()
			for _, t := range item.Tags {
				_ = h.dao.TorrentTag.WithContext(c.Request.Context()).Create(&model.TorrentTag{
					InfoHash: infoHash,
					Name:     t,
				})
			}
		}
	}

	if len(toDelete) > 0 {
		_ = h.blocking.Block(c.Request.Context(), toDelete, true)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(items)})
}
