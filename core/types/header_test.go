/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */
package types

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology/common"
	"github.com/stretchr/testify/assert"
)

func TestHeader_Serialize(t *testing.T) {
	header := Header{}
	header.Height = 321
	header.Bookkeepers = make([]keypair.PublicKey, 0)
	header.SigData = make([][]byte, 0)
	sink := common.NewZeroCopySink(0)
	header.Serialization(sink)
	bs := sink.Bytes()

	var h2 Header
	source := common.NewZeroCopySource(bs)
	err := h2.Deserialization(source)
	assert.Nil(t, err)
	assert.Equal(t, fmt.Sprint(header), fmt.Sprint(h2))

	var h3 RawHeader
	source = common.NewZeroCopySource(bs)
	err = h3.Deserialization(source)
	assert.Nil(t, err)
	assert.Equal(t, header.Height, h3.Height)
	assert.Equal(t, bs, h3.Payload)

	buf := common.SerializeToBytes(&h3)
	assert.Equal(t, buf, bs)
}

func TestHeader_SerializeShardHeader(t *testing.T) {
	header := Header{}
	header.Version = common.VERSION_SUPPORT_SHARD
	header.Height = 321
	header.Bookkeepers = make([]keypair.PublicKey, 0)
	var bookkeeper common.Address
	_, _ = rand.Read(bookkeeper[:])
	header.NextBookkeeper = bookkeeper
	header.SigData = make([][]byte, 0)
	sink := common.NewZeroCopySink(0)
	header.Serialization(sink)
	bs := sink.Bytes()

	var h2 Header
	source := common.NewZeroCopySource(bs)
	err := h2.Deserialization(source)
	assert.Nil(t, err)
	assert.Equal(t, fmt.Sprint(header), fmt.Sprint(h2))

	var h3 RawHeader
	source = common.NewZeroCopySource(bs)
	err = h3.Deserialization(source)
	assert.Nil(t, err)
	assert.Equal(t, header.Height, h3.Height)
	assert.Equal(t, bs, h3.Payload)

	buf := common.SerializeToBytes(&h3)
	assert.Equal(t, buf, bs)

}
