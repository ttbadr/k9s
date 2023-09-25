package view

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/model"
	"github.com/derailed/k9s/internal/port"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
)

const containerTitle = "Containers"

// Container represents a container view.
type Container struct {
	ResourceViewer
}

// NewContainer returns a new container view.
func NewContainer(gvr client.GVR) ResourceViewer {
	c := Container{}
	c.ResourceViewer = NewLogsExtender(NewBrowser(gvr), c.logOptions)
	c.SetEnvFn(c.k9sEnv)
	c.GetTable().SetEnterFn(c.viewLogs)
	c.GetTable().SetDecorateFn(c.decorateRows)
	c.AddBindKeysFn(c.bindKeys)
	c.GetTable().SetDecorateFn(c.portForwardIndicator)

	return &c
}

func (c *Container) portForwardIndicator(data *render.TableData) {
	ff := c.App().factory.Forwarders()
	col := data.IndexOfHeader("PF")
	for _, re := range data.RowEvents {
		if ff.IsContainerForwarded(c.GetTable().Path, re.Row.ID) {
			re.Row.Fields[col] = "[orange::b]Ⓕ"
		}
	}
}

func (c *Container) decorateRows(data *render.TableData) {
	decorateCpuMemHeaderRows(c.App(), data)
}

// Name returns the component name.
func (c *Container) Name() string { return containerTitle }

func (c *Container) bindDangerousKeys(aa ui.KeyActions) {
	aa.Add(ui.KeyActions{
		ui.KeyS: ui.NewKeyAction("Shell", c.shellCmd, true),
		ui.KeyT: ui.NewKeyAction("Attach", c.attachCmd, true),
		ui.KeyV: ui.NewKeyAction("Less log", c.lessCmd, true),
		ui.KeyA: ui.NewKeyAction("Arthas", c.arthasCmd, true),
	})
}

func (c *Container) bindKeys(aa ui.KeyActions) {
	aa.Delete(tcell.KeyCtrlSpace, ui.KeySpace)

	if !c.App().Config.K9s.IsReadOnly() {
		c.bindDangerousKeys(aa)
	}

	aa.Add(ui.KeyActions{
		ui.KeyF:      ui.NewKeyAction("Show PortForward", c.showPFCmd, true),
		ui.KeyShiftF: ui.NewKeyAction("PortForward", c.portFwdCmd, true),
		ui.KeyShiftT: ui.NewKeyAction("Sort Restart", c.GetTable().SortColCmd("RESTARTS", false), false),
	})
	aa.Add(resourceSorters(c.GetTable()))
}

func (c *Container) k9sEnv() Env {
	path := c.GetTable().GetSelectedItem()
	row, ok := c.GetTable().GetSelectedRow(path)
	if !ok {
		log.Error().Msgf("unable to locate selected row for %q", path)
	}
	env := defaultEnv(c.App().Conn().Config(), path, c.GetTable().GetModel().Peek().Header, row)
	env["NAMESPACE"], env["POD"] = client.Namespaced(c.GetTable().Path)

	return env
}

func (c *Container) logOptions(prev bool) (*dao.LogOptions, error) {
	path := c.GetTable().GetSelectedItem()
	if path == "" {
		return nil, errors.New("nothing selected")
	}

	cfg := c.App().Config.K9s.Logger
	opts := dao.LogOptions{
		Path:            c.GetTable().Path,
		Container:       path,
		Lines:           int64(cfg.TailCount),
		SinceSeconds:    cfg.SinceSeconds,
		SingleContainer: true,
		ShowTimestamp:   cfg.ShowTime,
		Previous:        prev,
	}

	return &opts, nil
}

func (c *Container) viewLogs(app *App, model ui.Tabular, gvr, path string) {
	c.ResourceViewer.(*LogsExtender).showLogs(c.GetTable().Path, false)
}

// Handlers...

func (c *Container) showPFCmd(evt *tcell.EventKey) *tcell.EventKey {
	path := c.GetTable().GetSelectedItem()
	if path == "" {
		return evt
	}

	if !c.App().factory.Forwarders().IsContainerForwarded(c.GetTable().Path, path) {
		c.App().Flash().Errf("no port-forward defined")
		return nil
	}
	pf := NewPortForward(client.NewGVR("portforwards"))
	pf.SetContextFn(c.portForwardContext)
	if err := c.App().inject(pf, false); err != nil {
		c.App().Flash().Err(err)
	}

	return nil
}

func (c *Container) portForwardContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, internal.KeyBenchCfg, c.App().BenchFile)
	return context.WithValue(ctx, internal.KeyPath, c.GetTable().Path)
}

func (c *Container) shellCmd(evt *tcell.EventKey) *tcell.EventKey {
	return c.shellWithCmd(evt, shellCheck)
}

func (c *Container) lessCmd(evt *tcell.EventKey) *tcell.EventKey {
	file := c.getSidecarLogPath()
	var cmd string
	if len(file) == 0 {
		c.ResourceViewer.(*LogsExtender).showLogs(c.GetTable().Path, false)
		return nil
	} else {
		cmd = "less -F " + file
		return c.shellWithCmd(evt, cmd)
	}
}

func (c *Container) arthasCmd(evt *tcell.EventKey) *tcell.EventKey {
	fileName := "arthas-bin.tgz"
	var address string
	if checkAddress("10.116.53.198") {
		address = "http://10.116.53.198/scripts/repo/" + fileName
	} else if checkAddress("github.com") {
		user, repo := "ttbadr", "arthas"
		ver, err := model.FetchLatestRev(user, repo)
		if err != nil {
			c.App().Flash().Errf("Could not get latest arthas version")
			return nil
		}
		address = fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", user, repo, ver, fileName)
	} else {
		c.App().Flash().Errf("Could not access arthas file server")
		return nil
	}
	log.Info().Msgf("Downloading arthas from %s", address)

	arthasCmd := "fileName=" + fileName + "\nurl=" + address + `
unset -v _JAVA_OPTIONS
home=/tmp/arthas
if [ ! -d $home ]; then
	mkdir $home
	echo "Downloading arthas from $url"
	if command -v wget &>/dev/null; then
		wget $url -q -O $home/$fileName || (rm -rf $home;echo 'Failed to download arthas with wget';sleep 5;)
	else
		curl -L $url -s -o $home/$fileName || (rm -rf $home;echo 'Failed to download arthas with curl';sleep 5;)
	fi
	cd $home && tar -xf $fileName || (rm -rf $home;echo 'Failed to extract arthas';sleep 5;)
	rm -f $fileName
fi
cd $home && java -jar arthas-boot.jar`

	return c.shellWithCmd(evt, arthasCmd)
}

func checkAddress(ipOrDomain string) bool {
	conn, err := net.DialTimeout("tcp", ipOrDomain+":80", 6*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (c *Container) shellWithCmd(evt *tcell.EventKey, cmd string) *tcell.EventKey {
	path := c.GetTable().GetSelectedItem()
	if path == "" {
		return evt
	}

	c.Stop()
	defer c.Start()
	shellInWithCmd(c.App(), c.GetTable().Path, path, cmd)

	return nil
}

func (c *Container) attachCmd(evt *tcell.EventKey) *tcell.EventKey {
	sel := c.GetTable().GetSelectedItem()
	if sel == "" {
		return evt
	}

	c.Stop()
	defer c.Start()
	attachIn(c.App(), c.GetTable().Path, sel)

	return nil
}

func (c *Container) portFwdCmd(evt *tcell.EventKey) *tcell.EventKey {
	path := c.GetTable().GetSelectedItem()
	if path == "" {
		return evt
	}

	if _, ok := c.App().factory.ForwarderFor(fwFQN(c.GetTable().Path, path)); ok {
		c.App().Flash().Err(fmt.Errorf("A port-forward already exists on container %s", c.GetTable().Path))
		return nil
	}

	ports, ann, ok := c.listForwardable(path)
	if !ok {
		return nil
	}
	ShowPortForwards(c, c.GetTable().Path+"|"+path, ports, ann, startFwdCB)

	return nil
}

func checkRunningStatus(co string, ss []v1.ContainerStatus) error {
	var cs *v1.ContainerStatus
	for i := range ss {
		if ss[i].Name == co {
			cs = &ss[i]
			break
		}
	}
	if cs == nil {
		return fmt.Errorf("unable to locate container status for %q", co)
	}

	if render.ToContainerState(cs.State) != "Running" {
		return fmt.Errorf("Container %s is not running?", co)
	}

	return nil
}

func locateContainer(co string, cc []v1.Container) (*v1.Container, error) {
	for i := range cc {
		if cc[i].Name == co {
			return &cc[i], nil
		}
	}
	return nil, fmt.Errorf("unable to locate container named %q", co)
}

func (c *Container) listForwardable(path string) (port.ContainerPortSpecs, map[string]string, bool) {
	po, err := fetchPod(c.App().factory, c.GetTable().Path)
	if err != nil {
		return nil, nil, false
	}

	co, err := locateContainer(path, po.Spec.Containers)
	if err != nil {
		c.App().Flash().Err(err)
		return nil, nil, false
	}

	if err := checkRunningStatus(path, po.Status.ContainerStatuses); err != nil {
		c.App().Flash().Err(err)
		return nil, nil, false
	}

	return port.FromContainerPorts(path, co.Ports), po.Annotations, true
}

func (c *Container) getSidecarLogPath() string {
	po, err := fetchPod(c.App().factory, c.GetTable().Path)
	if err != nil {
		return ""
	}

	co, err := locateContainer(c.GetTable().GetSelectedItem(), po.Spec.Containers)
	if err != nil {
		c.App().Flash().Err(err)
		return ""
	}

	if len(co.Args) == 3 && strings.Contains(co.Name, "sidecar") {
		strs := strings.Split(co.Args[2], "tail -n+1 -F")
		if len(strs) > 1 {
			return strs[len(strs)-1]
		}
	}
	return ""
}
