package view

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// Secret presents a secret viewer.
type Secret struct {
	ResourceViewer
}

// NewSecret returns a new viewer.
func NewSecret(gvr client.GVR) ResourceViewer {
	s := Secret{
		ResourceViewer: NewBrowser(gvr),
	}
	s.GetTable().SetEnterFn(s.decodeEnter)
	s.AddBindKeysFn(s.bindKeys)

	return &s
}

func (s *Secret) bindKeys(aa ui.KeyActions) {
	aa.Add(ui.KeyActions{
		ui.KeyU: ui.NewKeyAction("UsedBy", s.refCmd, true),
	})
}

func (s *Secret) decodeEnter(app *App, model ui.Tabular, gvr, path string) {
	s.decode()
}

func (s *Secret) refCmd(evt *tcell.EventKey) *tcell.EventKey {
	return scanRefs(evt, s.App(), s.GetTable(), "v1/secrets")
}

func (s *Secret) decodeCmd(evt *tcell.EventKey) *tcell.EventKey {
	s.decode()
	return nil
}

func (s *Secret) decode() {
	path := s.GetTable().GetSelectedItem()
	if path == "" {
		return
	}

	o, err := s.App().factory.Get(s.GVR().String(), path, true, labels.Everything())
	if err != nil {
		s.App().Flash().Err(err)
		return
	}

	var secret v1.Secret
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &secret)
	if err != nil {
		s.App().Flash().Err(err)
		return
	}

	var bd strings.Builder
	for k, val := range secret.Data {
		bd.WriteString(k)
		if len(val) > 0 {
			// show base64 string if the secret is not a text file
			if isBinary(val) {
				bd.WriteString(" : <Not decoded for a binary file>\n")
				bd.WriteString(base64.StdEncoding.EncodeToString(val))
			} else {
				s := strings.TrimRight(string(val), "\n")
				bd.WriteString(" : ")
				if strings.Contains(s, "\n") {
					bd.WriteString("\n")
				}
				bd.WriteString(s)
			}
			bd.WriteString("\n")
		} else {
			bd.WriteString(" :\n")
		}
		bd.WriteString("\n")
	}

	details := NewDetails(s.App(), "Secret Decoder", path, true).Update(bd.String())
	if err := s.App().inject(details, false); err != nil {
		s.App().Flash().Err(err)
	}
}

func isBinary(decoded []byte) bool {
	return http.DetectContentType(decoded) == "application/octet-stream"
}
