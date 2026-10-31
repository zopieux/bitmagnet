package model

// ContentType represents the type of content
// ENUM(movie, tv_show, anime, porn, music, audiobook, podcast, book, software, game, education)
type ContentType string

func (c ContentType) Label() string {
	return c.String()
}

func (c ContentType) IsNil() bool {
	return c == ""
}

func (c ContentType) IsVideo() bool {
	return c == ContentTypeMovie || c == ContentTypeTvShow || c == ContentTypeAnime || c == ContentTypePorn
}
