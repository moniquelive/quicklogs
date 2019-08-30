package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"sort"

	"github.com/docker/cli/cli/compose/convert"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var (
	app              *tview.Application
	streamCancelFunc context.CancelFunc
	cli              *client.Client
)

func init() {
	var err error
	cli, err = client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatal("unable to create docker client:", err)
	}
	app = tview.NewApplication()
}

func main() {
	if err := browser(); err != nil {
		log.Fatal("browser error:", err)
	}
	if err := app.Run(); err != nil {
		log.Fatal("app.Run error:", err)
	}
}

func browser() error {
	// Create the basic objects.
	txtLogs := tview.NewTextView().SetDynamicColors(true)
	txtLogs.SetBorder(true).SetTitle("tail -f (from 1h ago)")

	lstServices := tview.NewList().ShowSecondaryText(false)
	lstServices.SetBorder(true).SetTitle("Services")

	lstServices.SetDoneFunc(func() {
		app.Stop()
	})

	txtLogs.SetDoneFunc(func(_ tcell.Key) {
		txtLogs.Clear()
		app.SetFocus(lstServices)
	}).SetChangedFunc(func() {
		app.Draw()
	})

	// Create the layout.
	pages := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(lstServices, 0, 1, true).
		AddItem(txtLogs, 0, 4, false)

	// create map: stackName -> [service1, service2, ...]
	stackNames, stackServices, err := extractStackAndServices()
	if err != nil {
		return err
	}
	if len(stackNames) == 0 {
		return fmt.Errorf("no stacks found!")
	}

	containerLogsOptions := types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Since:      "10m",
	}

	for _, stackName := range stackNames {
		for _, serviceName := range stackServices[stackName] {
			lstServices.AddItem(serviceName, "", 0, nil)
		}
		// When the user selects a service, show its log.
		lstServices.SetChangedFunc(func(i int, serviceName string, t string, s rune) {
			cancelLogStream()
			txtLogs.Clear()

			var ctx context.Context
			ctx, streamCancelFunc = context.WithCancel(context.Background())

			logStream, err := cli.ServiceLogs(ctx, serviceName, containerLogsOptions)
			if err != nil {
				txtLogs.Write([]byte(fmt.Sprintf("error opening logs for service %s: %v", serviceName, err)))
				return
			}

			go io.Copy(tview.ANSIWriter(txtLogs), logStream)
			//app.SetFocus(txtLogs)
			//txtLogs.ScrollToEnd()
		})
	}

	lstServices.SetCurrentItem(0)
	app.SetRoot(pages, true)

	return nil
}

func cancelLogStream() {
	if streamCancelFunc != nil {
		streamCancelFunc()
		streamCancelFunc = nil
	}
}

func extractStackAndServices() (stackNames []string, stackServices map[string][]string, err error) {
	// list swarm services
	allStacksFilter := filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace"))
	allServices, err := cli.ServiceList(context.Background(), types.ServiceListOptions{Filters: allStacksFilter})
	if err != nil {
		return
	}

	stackServices = make(map[string][]string, 0)
	for _, service := range allServices {
		stackName, ok := service.Spec.Labels[convert.LabelNamespace]
		if !ok {
			err = fmt.Errorf("cannot get label %s for service %s", convert.LabelNamespace, service.ID)
			return
		}

		if _, ok := stackServices[stackName]; !ok {
			// we never heard about this stack! save it...
			stackNames = append(stackNames, stackName)
		}
		stackServices[stackName] = append(stackServices[stackName], service.Spec.Name)
	}

	for _, services := range stackServices {
		sort.Strings(services)
	}
	sort.Strings(stackNames)
	return stackNames, stackServices, err
}
