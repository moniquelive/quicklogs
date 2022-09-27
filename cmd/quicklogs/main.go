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
  "github.com/docker/docker/api/types/swarm"
  "github.com/docker/docker/client"
  "github.com/gdamore/tcell/v2"
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
    log.Fatalln("unable to create docker client:", err)
  }
  app = tview.NewApplication()
}

func main() {
  if err := browser(); err != nil {
    log.Fatalln("browser error:", err)
  }
  if err := app.Run(); err != nil {
    log.Fatalln("app.Run error:", err)
  }
}

func browser() error {
  // Create the basic objects.
  lstStacks := tview.
    NewList().
    ShowSecondaryText(false).
    SetDoneFunc(func() { app.Stop() })
  lstStacks.
    SetBorder(true).
    SetTitle("Stacks")

  lstServices := tview.
    NewList().
    ShowSecondaryText(false).
    SetSelectedFocusOnly(true).
    SetDoneFunc(func() { app.Stop() })
  lstServices.
    SetBorder(true).
    SetTitle("Services")

  txtLogs := tview.
    NewTextView().
    SetDynamicColors(true).
    SetChangedFunc(func() { app.Draw() })
  txtLogs.
    SetBorder(true).
    SetTitle("tail -f (starting 10min ago)")

  // set up key events
  lstStacks.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    if event.Key() == tcell.KeyRight || event.Key() == tcell.KeyEnter {
      app.SetFocus(lstServices)
      return nil
    }
    return event
  })

  lstServices.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    if event.Key() == tcell.KeyLeft ||
      event.Key() == tcell.KeyESC {
      app.SetFocus(lstStacks)
      return nil
    }
    if event.Key() == tcell.KeyEnter {
      app.SetFocus(txtLogs)
      return nil
    }
    return event
  })

  txtLogs.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    if event.Key() == tcell.KeyESC {
      app.SetFocus(lstServices)
      return nil
    }
    return event
  })

  // Create the layout.
  top := tview.NewFlex().
    AddItem(lstStacks, 0, 1, true).
    AddItem(lstServices, 0, 4, false)
  pages := tview.NewFlex().
    SetDirection(tview.FlexRow).
    AddItem(top, 0, 1, true).
    AddItem(txtLogs, 0, 4, false)

  // create map: stackName -> [service1, service2, ...]
  stackNames, stackServices, err := extractStackAndServices()
  if err != nil {
    return err
  }
  if len(stackNames) == 0 {
    return fmt.Errorf("no docker swarm stacks found")
  }

  containerLogsOptions := types.ContainerLogsOptions{
    ShowStdout: true,
    ShowStderr: true,
    Timestamps: true,
    Follow:     true,
    Since:      "10m",
  }

  // When the user selects a stack, show its services.
  for _, stackName := range stackNames {
    lstStacks.AddItem(stackName, "", 0, nil)
  }
  lstStacks.SetChangedFunc(func(i int, stackName string, secondaryLabel string, s rune) {
    lstServices.Clear()
    // adds services of this stack
    for _, serviceName := range stackServices[stackName] {
      lstServices.AddItem(serviceName, "", 0, nil)
    }
  })
  // force change event
  lstStacks.SetCurrentItem(-1)
  lstStacks.SetCurrentItem(0)

  // When the user selects a service, show its log.
  lstServices.SetChangedFunc(func(_ int, serviceName string, _ string, _ rune) {
    if cancel != nil {
      cancel()
    }
    txtLogs.Clear()

    var ctx context.Context
    ctx, cancel = context.WithCancel(context.Background())

    logStream, err := cli.ServiceLogs(ctx, serviceName, containerLogsOptions)
    if err != nil {
      errMsg := fmt.Sprintf("error opening logs for service %s: %v", serviceName, err)
      _, _ = txtLogs.Write([]byte(errMsg))
      return
    }

    go io.Copy(tview.ANSIWriter(txtLogs), logStream)
  })
  // force change event
  lstServices.SetCurrentItem(-1)
  lstServices.SetCurrentItem(0)

  app.SetRoot(pages, true)
  return nil
}

func extractStackAndServices() (stackNames []string, stackServices map[string][]string, err error) {
  // list swarm services
  var allServices []swarm.Service
  allServices, err = cli.ServiceList(
    context.Background(),
    types.ServiceListOptions{
      Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace"))})
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
