package classify

import (
	"github.com/bitmagnet-io/bitmagnet/internal/classify/parsers"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

// Result holds the classification result for a torrent.
// It contains parsed attributes from the torrent name and optionally
// content matched from TMDB.
type Result struct {
	ContentType     model.NullContentType
	BaseTitle       model.NullString
	Date            model.Date
	Languages       model.Languages
	LanguageMulti   bool
	Episodes        model.Episodes
	VideoResolution model.NullVideoResolution
	VideoSource     model.NullVideoSource
	VideoCodec      model.NullVideoCodec
	Video3D         model.NullVideo3D
	VideoModifier   model.NullVideoModifier
	ReleaseGroup    model.NullString
	Content         *model.Content
	Tags            map[string]struct{}
}

// MergeAttributes copies parsed attributes into r where r doesn't already have them set.
func (r *Result) MergeAttributes(a parsers.Attributes) {
	if !r.ContentType.Valid {
		r.ContentType = a.ContentType
	}

	if !r.BaseTitle.Valid {
		r.BaseTitle = a.BaseTitle
	}

	if r.Date.IsNil() {
		r.Date = a.Date
	}

	if len(r.Languages) == 0 {
		r.Languages = a.Languages
	}

	r.LanguageMulti = r.LanguageMulti || a.LanguageMulti
	if len(r.Episodes) == 0 {
		r.Episodes = a.Episodes
	}

	if !r.VideoResolution.Valid {
		r.VideoResolution = a.VideoResolution
	}

	if !r.VideoSource.Valid {
		r.VideoSource = a.VideoSource
	}

	if !r.VideoCodec.Valid {
		r.VideoCodec = a.VideoCodec
	}

	if !r.Video3D.Valid {
		r.Video3D = a.Video3D
	}

	if !r.VideoModifier.Valid {
		r.VideoModifier = a.VideoModifier
	}

	if !r.ReleaseGroup.Valid {
		r.ReleaseGroup = a.ReleaseGroup
	}
}

// AttachContent attaches matched content, updating ContentType and Languages.
func (r *Result) AttachContent(content *model.Content) {
	r.Content = content
	r.ContentType = model.NewNullContentType(content.Type)

	if content.OriginalLanguage.Valid {
		if len(r.Languages) == 0 || r.LanguageMulti {
			if r.Languages == nil {
				r.Languages = make(model.Languages)
			}

			r.Languages[content.OriginalLanguage.Language] = struct{}{}
		}
	}
}
