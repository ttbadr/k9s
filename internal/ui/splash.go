package ui

import (
	"fmt"
	"strings"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/tview"
)

// LogoSmall K9s small log.
var LogoSmall = []string{
	`                       `,
	`  ____   _____   ______`,
	`_/ ___\ /     \ /  ___/`,
	`\  \___|  Y Y  \\___ \ `,
	` \___  >__|_|  /____  >`,
	`     \/      \/     \/ `,
}

// LogoBig K9s big logo for splash page.
var LogoBig = []string{
	`                                                .__ `,
	`  ___________     _____   __ __ _______ _____   |__|`,
	` /  ___/\__  \   /     \ |  |  \\_  __ \\__  \  |  |`,
	` \___ \  / __ \_|  Y Y  \|  |  / |  | \/ / __ \_|  |`,
	`/____  >(____  /|__|_|  /|____/  |__|   (____  /|__|`,
	`     \/      \/       \/                     \/     `,
}

// Splash represents a splash screen.
type Splash struct {
	*tview.Flex
}

// NewSplash instantiates a new splash screen with product and company info.
func NewSplash(styles *config.Styles, version string) *Splash {
	s := Splash{Flex: tview.NewFlex()}
	s.SetBackgroundColor(styles.BgColor())

	logo := tview.NewTextView()
	logo.SetDynamicColors(true)
	logo.SetTextAlign(tview.AlignCenter)
	s.layoutLogo(logo, styles)

	vers := tview.NewTextView()
	vers.SetDynamicColors(true)
	vers.SetTextAlign(tview.AlignCenter)
	s.layoutRev(vers, version, styles)

	s.SetDirection(tview.FlexRow)
	s.AddItem(logo, 10, 1, false)
	s.AddItem(vers, 1, 1, false)

	return &s
}

func (s *Splash) layoutLogo(t *tview.TextView, styles *config.Styles) {
	logo := strings.Join(LogoBig, fmt.Sprintf("\n[%s::b]", styles.Body().LogoColor))
	fmt.Fprintf(t, "%s[%s::b]%s\n",
		strings.Repeat("\n", 2),
		styles.Body().LogoColor,
		logo)
}

func (s *Splash) layoutRev(t *tview.TextView, rev string, styles *config.Styles) {
	fmt.Fprintf(t, "[%s::b]Revision [red::b]%s", styles.Body().FgColor, rev)
}
