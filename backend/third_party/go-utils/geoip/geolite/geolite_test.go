package geolite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeoLite(t *testing.T) {
	g, err := NewClient()
	assert.Nil(t, err)

	ret, err := g.Query("47.108.149.89")
	assert.Nil(t, err)
	assert.Equal(t, "中国", ret.Country)
	assert.Equal(t, "四川", ret.Province)
	assert.Equal(t, "成都", ret.City)

	ret, err = g.Query("::1")
	assert.Nil(t, err)

	ret, err = g.Query("127.0.0.1")
	assert.Nil(t, err)
}
