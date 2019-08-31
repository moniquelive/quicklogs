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
	"github.com/rivo/tview"
)

var (
	app    *tview.Application
	cancel context.CancelFunc
	cli    *client.Client
)

func init() {
	var err error
	cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
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
	txtLogs := tview.
		NewTextView().
		SetDynamicColors(true).
		SetChangedFunc(func() { app.Draw() })
	txtLogs.
		SetBorder(true).
		SetTitle("tail -f (starting 10min ago)")

	lstServices := tview.
		NewList().
		ShowSecondaryText(false).
		SetDoneFunc(func() { app.Stop() })
	lstServices.
		SetBorder(true).
		SetTitle("Services")

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
			if cancel != nil {
				cancel()
			}
			txtLogs.Clear()

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())

			logStream, err := cli.ServiceLogs(ctx, serviceName, containerLogsOptions)
			if err != nil {
				_, _ = txtLogs.Write([]byte(fmt.Sprintf("error opening logs for service %s: %v", serviceName, err)))
				return
			}

			go io.Copy(tview.ANSIWriter(txtLogs), logStream)
		})
	}
	// force change event
	lstServices.SetCurrentItem(-1)
	lstServices.SetCurrentItem(0)

	app.SetRoot(pages, true)

	return nil
}

func extractStackAndServices() (stackNames []string, stackServices map[string][]string, err error) {
	// list swarm services
	includeStackName := filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace"))
	allServices, err := cli.ServiceList(context.Background(), types.ServiceListOptions{Filters: includeStackName})
	if err != nil {
		return
	}
	// foreach swarm service
	stackServices = make(map[string][]string)
	for _, service := range allServices {
		stackName, ok := service.Spec.Labels[convert.LabelNamespace]
		if !ok {
			err = fmt.Errorf("cannot get label %s for service %s", convert.LabelNamespace, service.ID)
			return
		}
		// if it's a new stack name, keep it
		_, ok = stackServices[stackName]
		if !ok {
			stackNames = append(stackNames, stackName)
		}
		// save service name on stack => services map
		stackServices[stackName] = append(stackServices[stackName], service.Spec.Name)
	}
	// sort service names
	for _, services := range stackServices {
		sort.Strings(services)
	}
	// sort stack names
	sort.Strings(stackNames)
	return stackNames, stackServices, err
}
