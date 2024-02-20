/*
 * BSD-3-Clause
 * Copyright 2021 sot (PR_713, C_rho_272)
 * Redistribution and use in source and binary forms, with or without modification,
 * are permitted provided that the following conditions are met:
 * 1. Redistributions of source code must retain the above copyright notice,
 * this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright notice,
 * this list of conditions and the following disclaimer in the documentation and/or
 * other materials provided with the distribution.
 * 3. Neither the name of the copyright holder nor the names of its contributors
 * may be used to endorse or promote products derived from this software without
 * specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 * WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
 * IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING,
 * BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA,
 * OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY,
 * WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY
 * OF SUCH DAMAGE.
 */

package MTHelper

import (
	"io"
	"unicode/utf8"

	md "github.com/russross/blackfriday/v2"
	mt "github.com/zelenin/go-tdlib/client"
)

type TGRenderer struct {
	index          int
	entities       []*mt.TextEntity
	openedEntities map[md.NodeType]*mt.TextEntity
	prevNodeType   md.NodeType
}

func NewRenderer() *TGRenderer {
	return &TGRenderer{
		entities:       make([]*mt.TextEntity, 0, 10),
		openedEntities: make(map[md.NodeType]*mt.TextEntity),
	}
}

func (r *TGRenderer) GetEntities() []*mt.TextEntity {
	if r.openedEntities != nil {
		for _, v := range r.openedEntities {
			if v != nil {
				v.Length = int32(r.index) - v.Offset
				r.entities = append(r.entities, v)
			}
		}
		r.openedEntities = make(map[md.NodeType]*mt.TextEntity)
	}
	return r.entities
}

func (r *TGRenderer) RenderNode(w io.Writer, node *md.Node, entering bool) md.WalkStatus {
	s := string(node.Literal)
	l := 0
	for _, c := range s {
		if utf8.RuneLen(c) < 4 {
			l++
		} else {
			l += 2 // fix for extended unicode (i.e. emoji)
		}
	}
	nodeType := node.Type
	if entering {
		var t mt.TextEntityType
		switch nodeType {
		case md.Strong:
			t = &mt.TextEntityTypeBold{}
		case md.Emph:
			t = &mt.TextEntityTypeItalic{}
		case md.Del:
			t = &mt.TextEntityTypeStrikethrough{}
		case md.Heading:
			// heading for TG means hashtag in start of line.
			// we need to revert hash (deleted by parser) and, if prev node was not
			// paragraph - new line
			var prefix []byte
			if r.prevNodeType == md.Paragraph {
				prefix = []byte{'#'}
			} else {
				prefix = []byte{'\n', '#'}
			}
			if _, err := w.Write(prefix); err != nil {
				return md.Terminate
			}
			l++
		case md.Link:
			t = &mt.TextEntityTypeTextUrl{Url: string(node.Destination)}
		case md.Softbreak:
			fallthrough
		case md.Hardbreak:
			fallthrough
		case md.Paragraph:
			node.Literal = []byte{'\n'}
			l++
		case md.Code:
			e := &mt.TextEntity{
				Offset: int32(r.index),
				Length: int32(l),
				Type:   &mt.TextEntityTypeCode{},
			}
			r.entities = append(r.entities, e)
		case md.CodeBlock:
			var codeBlockType mt.TextEntityType
			if len(node.Info) > 0 {
				codeBlockType = &mt.TextEntityTypePreCode{Language: string(node.Info)}
			} else {
				codeBlockType = &mt.TextEntityTypePre{}
			}
			e := &mt.TextEntity{
				Offset: int32(r.index),
				Length: int32(l),
				Type:   codeBlockType,
			}
			r.entities = append(r.entities, e)
		}
		if t != nil {
			ent := &mt.TextEntity{
				Offset: int32(r.index),
				Length: 0,
				Type:   t,
			}
			r.openedEntities[nodeType] = ent
		}
	} else if ent := r.openedEntities[nodeType]; ent != nil {
		ent.Length = int32(r.index+l) - ent.Offset
		if ent.Length > 0 {
			r.entities = append(r.entities, ent)
		}
		r.openedEntities[nodeType] = nil
	}

	r.index += l
	r.prevNodeType = nodeType
	if _, err := w.Write(node.Literal); err == nil {
		return md.GoToNext
	} else {
		return md.Terminate
	}
}

func (r *TGRenderer) RenderHeader(io.Writer, *md.Node) {
}

func (r *TGRenderer) RenderFooter(io.Writer, *md.Node) {
}

func FormatText(t string) *mt.FormattedText {
	rnd := NewRenderer()
	data := md.Run([]byte(t), md.WithRenderer(rnd), md.WithExtensions(md.HardLineBreak|md.Strikethrough))
	out := mt.FormattedText{
		Text:     string(data),
		Entities: rnd.GetEntities(),
	}
	return &out
}
