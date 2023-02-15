package view

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/k9s/internal/ui/dialog"
	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"
	"github.com/rs/zerolog/log"
)

// ScaleExtender adds scaling extensions.
type ScaleExtender struct {
	ResourceViewer
}

// NewScaleExtender returns a new extender.
func NewScaleExtender(r ResourceViewer) ResourceViewer {
	s := ScaleExtender{ResourceViewer: r}
	s.AddBindKeysFn(s.bindKeys)

	return &s
}

func (s *ScaleExtender) bindKeys(aa ui.KeyActions) {
	if s.App().Config.K9s.IsReadOnly() {
		return
	}
	aa.Add(ui.KeyActions{
		ui.KeyF: ui.NewKeyAction("Scale-Restart", s.fullRestartCmd, true),
		ui.KeyS: ui.NewKeyAction("Scale", s.scaleCmd, true),
	})
}

func (s *ScaleExtender) fullRestartCmd(evt *tcell.EventKey) *tcell.EventKey {
	paths := s.GetTable().GetSelectedItems()
	if len(paths) == 0 || len(paths) > 1 || paths[0] == "" {
		return nil
	}

	s.Stop()
	defer s.Start()
	msg := fmt.Sprintf("Scale Restart %s %s?", singularize(s.GVR().R()), paths[0])
	dialog.ShowConfirm(s.App().Styles.Dialog(), s.App().Content.Pages, "Confirm Scale Restart", msg, func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.App().Conn().Config().CallTimeout())
		defer cancel()
		for _, path := range paths {
			factor, _ := s.getReplicas(paths)
			if factor == "0" {
				continue
			}
			if err := s.scale(ctx, path, 0); err != nil {
				s.App().Flash().Err(err)
			} else {
				s.App().Flash().Infof("Scale to 0 for `%s...", path)
			}
			count, _ := strconv.Atoi(factor)
			if err := s.scale(ctx, path, count); err != nil {
				s.App().Flash().Err(err)
			} else {
				s.App().Flash().Infof("Scale to %d for `%s...", count, path)
			}
		}
	}, func() {})

	return nil
}

func (s *ScaleExtender) scaleCmd(evt *tcell.EventKey) *tcell.EventKey {
	paths := s.GetTable().GetSelectedItems()
	if len(paths) == 0 {
		return nil
	}

	s.Stop()
	defer s.Start()
	s.showScaleDialog(paths)

	return nil
}

func (s *ScaleExtender) showScaleDialog(paths []string) {
	form, err := s.makeScaleForm(paths)
	if err != nil {
		s.App().Flash().Err(err)
		return
	}
	confirm := tview.NewModalForm("<Scale>", form)
	msg := fmt.Sprintf("Scale %s %s?", singularize(s.GVR().R()), paths[0])
	if len(paths) > 1 {
		msg = fmt.Sprintf("Scale [%d] %s?", len(paths), s.GVR().R())
	}
	confirm.SetText(msg)
	confirm.SetDoneFunc(func(int, string) {
		s.dismissDialog()
	})
	s.App().Content.AddPage(scaleDialogKey, confirm, false, false)
	s.App().Content.ShowPage(scaleDialogKey)
}

func (s *ScaleExtender) valueOf(col string) (string, error) {
	colIdx, ok := s.GetTable().HeaderIndex(col)
	if !ok {
		return "", fmt.Errorf("no column index for %s", col)
	}
	return s.GetTable().GetSelectedCell(colIdx), nil
}

func (s *ScaleExtender) getReplicas(sels []string) (string, error) {
	if len(sels) == 1 {
		replicas, err := s.valueOf("READY")
		if err != nil {
			return "0", err
		}
		tokens := strings.Split(replicas, "/")
		if len(tokens) < 2 {
			return "0", fmt.Errorf("unable to locate replicas from %s", replicas)
		}
		return strings.TrimRight(tokens[1], ui.DeltaSign), nil
	}
	return "0", nil
}

func (s *ScaleExtender) makeScaleForm(sels []string) (*tview.Form, error) {
	f := s.makeStyledForm()

	factor, err := s.getReplicas(sels)
	if err != nil {
		return nil, err
	}
	f.AddInputField("Replicas:", factor, 4, func(textToCheck string, lastChar rune) bool {
		_, err := strconv.Atoi(textToCheck)
		return err == nil
	}, func(changed string) {
		factor = changed
	})

	f.AddButton("OK", func() {
		defer s.dismissDialog()
		count, err := strconv.Atoi(factor)
		if err != nil {
			s.App().Flash().Err(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.App().Conn().Config().CallTimeout())
		defer cancel()
		for _, sel := range sels {
			if err := s.scale(ctx, sel, count); err != nil {
				log.Error().Err(err).Msgf("DP %s scaling failed", sel)
				s.App().Flash().Err(err)
				return
			}
		}
		if len(sels) == 1 {
			s.App().Flash().Infof("[%d] %s scaled successfully", len(sels), singularize(s.GVR().R()))
		} else {
			s.App().Flash().Infof("%s %s scaled successfully", s.GVR().R(), sels[0])
		}
	})

	f.AddButton("Cancel", func() {
		s.dismissDialog()
	})

	return f, nil
}

func (s *ScaleExtender) dismissDialog() {
	s.App().Content.RemovePage(scaleDialogKey)
}

func (s *ScaleExtender) makeStyledForm() *tview.Form {
	f := tview.NewForm()
	f.SetItemPadding(0)
	f.SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
		SetButtonTextColor(tview.Styles.PrimaryTextColor).
		SetLabelColor(tcell.ColorAqua).
		SetFieldTextColor(tcell.ColorOrange)

	return f
}

func (s *ScaleExtender) scale(ctx context.Context, path string, replicas int) error {
	res, err := dao.AccessorFor(s.App().factory, s.GVR())
	if err != nil {
		return err
	}
	scaler, ok := res.(dao.Scalable)
	if !ok {
		return fmt.Errorf("expecting a scalable resource for %q", s.GVR())
	}

	return scaler.Scale(ctx, path, int32(replicas))
}
