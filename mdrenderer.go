/*
 * BSD-3-Clause
 * Copyright 2020 sot (PR_713, C_rho_272)
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
import md "github.com/russross/blackfriday"
import mt "github.com/Arman92/go-tdlib"
import "io"

type TGRenderer struct{
	index          int
	entities       []mt.TextEntity
	openedEntities map[md.NodeType]*mt.TextEntity
}

func NewRenderer() *TGRenderer{
	return &TGRenderer{
		entities:       make([]mt.TextEntity, 0, 10),
		openedEntities: make(map[md.NodeType]*mt.TextEntity),
	}
}

func (r *TGRenderer) GetEntities() []mt.TextEntity{
	if r.openedEntities != nil {
		for _, v := range r.openedEntities {
			if v != nil {
				v.Length = int32(r.index) - v.Offset
				r.entities = append(r.entities, *v)
			}
		}
		r.openedEntities = make(map[md.NodeType]*mt.TextEntity)
	}
	return r.entities
}

func (r *TGRenderer) RenderNode(w io.Writer, node *md.Node, entering bool) md.WalkStatus{
	s := string(node.Literal)
	l := len(s)
	if entering {
		var t mt.TextEntityType
		var extra string
		switch node.Type {
		case md.Strong:
			t = mt.NewTextEntityTypeBold()
		case md.Emph:
			t = mt.NewTextEntityTypeItalic()
		case md.Del:
			t = mt.NewTextEntityTypeCashtag() //TODO: check
		case md.Link:
			t = mt.NewTextEntityTypeTextURL(string(node.Destination))
		case md.Code:
			r.entities = append(r.entities, *mt.NewTextEntity(int32(r.index), int32(l), mt.NewTextEntityTypeCode()))
		case md.CodeBlock:
			var codeBlockType mt.TextEntityType
			if len(node.Info) > 0{
				codeBlockType = mt.NewTextEntityTypePreCode(string(node.Info))
			} else{
				codeBlockType = mt.NewTextEntityTypePre()
			}
			r.entities = append(r.entities, *mt.NewTextEntity(int32(r.index), int32(l), codeBlockType))
		}
		if t != nil{
			ent := mt.NewTextEntity(int32(r.index), 0, t)
			ent.Extra = extra
			r.openedEntities[node.Type] = mt.NewTextEntity(int32(r.index), 0, t)
		}
	} else{
		if ent := r.openedEntities[node.Type]; ent != nil{
			ent.Length = int32(r.index+ l) - ent.Offset
			r.entities = append(r.entities, *ent)
			r.openedEntities[node.Type] = nil
		}
	}

	r.index += l
	if _, err := w.Write(node.Literal); err == nil{
		return md.GoToNext
	} else {
		return md.Terminate
	}
}

func (r *TGRenderer) RenderHeader(w io.Writer, node *md.Node){
	_, _ = w.Write(node.Literal)
}

func (r *TGRenderer) RenderFooter(w io.Writer, node *md.Node){
	_, _ = w.Write(node.Literal)
}

func FormatText(t string) *mt.FormattedText {
	rnd := NewRenderer()
	data := md.Run([]byte(t), md.WithRenderer(rnd))
	out := mt.NewFormattedText(string(data), rnd.GetEntities())
	return out
}
