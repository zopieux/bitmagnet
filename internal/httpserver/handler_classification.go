package httpserver

import (
	"database/sql/driver"
	"net/http"
	"strconv"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	InfoHash string   `json:"infoHash"`
	Action   string   `json:"action"` // "do_nothing", "delete", or "archive"
	Tags     []string `json:"tags"`
	// ContentType, when present, replaces the torrent's content type.
	// Must be a valid content type value (e.g. "movie", "music", "xxx");
	// invalid values are ignored. This lets an external classifier
	// override the builtin classification after the fact.
	ContentType *string `json:"contentType"`
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
		db = db.Where("info_hash IN (SELECT info_hash FROM torrent_tags WHERE name = ?)", tag)
	}

	if excludeTag != "" {
		db = db.Where("info_hash NOT IN (SELECT info_hash FROM torrent_tags WHERE name = ?)", excludeTag)
	}

	var results []model.TorrentContent
	err := db.
		Preload("Torrent").
		Preload("Torrent.Files").
		Preload("Torrent.Tags").
		Preload("Torrent.Sources").
		Preload("Torrent.Hint").
		Limit(limit).
		Find(&results).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for i := range results {
		if len(results[i].Torrent.Files) == 0 && results[i].Torrent.Name != "" {
			results[i].Torrent.Files = []model.TorrentFile{
				{
					InfoHash:  results[i].Torrent.InfoHash,
					Index:     0,
					Path:      results[i].Torrent.Name,
					Extension: results[i].Torrent.Extension,
					Size:      results[i].Torrent.Size,
				},
			}
		}
		if results[i].Torrent.FilesSummary == nil && results[i].Torrent.FilesStatus == model.FilesStatusSingle && results[i].Torrent.Name != "" {
			s := model.NewSingleFileSummary(results[i].Torrent.Name, uint64(results[i].Torrent.Size), results[i].Torrent.Extension.String)
			results[i].Torrent.FilesSummary = &s
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items": results,
	})
}

func chunkSlice[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	var chunks [][]T
	for size < len(items) {
		items, chunks = items[size:], append(chunks, items[0:size:size])
	}
	return append(chunks, items)
}

func (h *ClassificationHandler) PatchBatch(c *gin.Context) {
	var items []PatchClassificationItem
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var toDelete []protocol.ID
	var toArchive []protocol.ID
	contentTypes := make(map[model.NullContentType][]driver.Valuer)
	var tagInfoHashes []driver.Valuer
	var tagsToInsert []*model.TorrentTag
	tagHashesSeen := make(map[protocol.ID]struct{})

	for _, item := range items {
		infoHash, err := protocol.ParseID(item.InfoHash)
		if err != nil {
			continue
		}
		if item.Action == "delete" {
			toDelete = append(toDelete, infoHash)
			continue
		}
		if item.Action == "archive" {
			toArchive = append(toArchive, infoHash)
			if item.ContentType == nil && item.Tags == nil {
				continue
			}
		}
		if item.ContentType != nil {
			if *item.ContentType == "" {
				contentTypes[model.NullContentType{}] = append(contentTypes[model.NullContentType{}], infoHash)
			} else if ct, parseErr := model.ParseContentType(*item.ContentType); parseErr == nil {
				contentTypes[model.NewNullContentType(ct)] = append(contentTypes[model.NewNullContentType(ct)], infoHash)
			}
		}
		if item.Tags != nil {
			if _, seen := tagHashesSeen[infoHash]; !seen {
				tagHashesSeen[infoHash] = struct{}{}
				tagInfoHashes = append(tagInfoHashes, infoHash)
			}
			tagMap := make(map[string]struct{}, len(item.Tags))
			for _, t := range item.Tags {
				if err := model.ValidateTagName(t); err != nil {
					continue
				}
				if _, ok := tagMap[t]; ok {
					continue
				}
				tagMap[t] = struct{}{}
				tagsToInsert = append(tagsToInsert, &model.TorrentTag{
					InfoHash: infoHash,
					Name:     t,
				})
			}
		}
	}

	db := h.dao.TorrentContent.WithContext(c.Request.Context()).UnderlyingDB()
	txErr := db.Transaction(func(tx *gorm.DB) error {
		for ct, hashes := range contentTypes {
			for _, chunk := range chunkSlice(hashes, 500) {
				if err := tx.Table("torrent_contents").
					Where("info_hash IN (?)", chunk).
					Update("content_type", ct).Error; err != nil {
					return err
				}
			}
		}

		if len(tagInfoHashes) > 0 {
			for _, chunk := range chunkSlice(tagInfoHashes, 500) {
				if err := tx.Table("torrent_tags").
					Where("info_hash IN (?)", chunk).
					Delete(&model.TorrentTag{}).Error; err != nil {
					return err
				}
			}
			if len(tagsToInsert) > 0 {
				// The torrent may have been deleted while this batch was in
				// flight; inserting a tag for it would violate the foreign key.
				existing := make(map[protocol.ID]struct{}, len(tagHashesSeen))
				for _, chunk := range chunkSlice(tagInfoHashes, 500) {
					var hashes []protocol.ID
					if err := tx.Table("torrents").
						Where("info_hash IN (?)", chunk).
						Pluck("info_hash", &hashes).Error; err != nil {
						return err
					}
					for _, hash := range hashes {
						existing[hash] = struct{}{}
					}
				}
				filtered := tagsToInsert[:0]
				for _, tag := range tagsToInsert {
					if _, ok := existing[tag.InfoHash]; ok {
						filtered = append(filtered, tag)
					}
				}
				tagsToInsert = filtered
			}
			if len(tagsToInsert) > 0 {
				if err := tx.Table("torrent_tags").
					Clauses(clause.OnConflict{DoNothing: true}).
					CreateInBatches(tagsToInsert, 500).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})

	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}

	if len(toArchive) > 0 {
		if err := h.blocking.Archive(c.Request.Context(), toArchive); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if len(toDelete) > 0 {
		_ = h.blocking.Block(c.Request.Context(), toDelete, true)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(items)})
}
