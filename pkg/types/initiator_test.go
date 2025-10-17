package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyTypeConstants(t *testing.T) {
	assert.Equal(t, "secp256k1", string(KeyTypeSecp256k1))
	assert.Equal(t, "ed25519", string(KeyTypeEd25519))
}
