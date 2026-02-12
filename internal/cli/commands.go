// Package cli implements the interactive command-line interface for Energizer.
// It provides tab-completion, live server status display, and all management
// commands that were available in the original Python CLI.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/server"
)

// CLI provides an interactive command-line interface.
type CLI struct {
	cfg      *config.Config
	eventBus *events.EventBus
	manager  *server.Manager
}

// NewCLI creates a new CLI handler.
func NewCLI(cfg *config.Config, eventBus *events.EventBus, manager *server.Manager) *CLI {
	return &CLI{
		cfg:      cfg,
		eventBus: eventBus,
		manager:  manager,
	}
}

// Start begins the interactive CLI loop.
func (c *CLI) Start(ctx context.Context) {
	fmt.Println("\nEnergizer CLI ready. Type 'help' for available commands.")
	fmt.Println("─────────────────────────────────────────────────────")

	// Simple line reader for cross-platform compatibility
	reader := newLineReader()
	if reader == nil {
		log.Warn().Msg("CLI: failed to initialize line reader, CLI disabled")
		<-ctx.Done()
		return
	}
	defer reader.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadLine("energizer> ")
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])
		args := parts[1:]

		if err := c.execute(ctx, cmd, args); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}
}

// execute processes a single CLI command.
func (c *CLI) execute(ctx context.Context, cmd string, args []string) error {
	switch cmd {
	case "help", "h", "?":
		c.printHelp()
	case "status", "s":
		c.printStatus(args)
	case "shutdown":
		return c.cmdShutdown(ctx, args)
	case "wake":
		return c.cmdWake(ctx, args)
	case "sleep":
		return c.cmdSleep(ctx, args)
	case "message", "msg":
		return c.cmdMessage(ctx, args)
	case "startup", "start":
		return c.cmdStartup(ctx, args)
	case "addservers":
		return c.cmdAddServers(ctx, args)
	case "reconnect":
		return c.cmdReconnect(ctx)
	case "setconfig":
		return c.cmdSetConfig(args)
	case "update":
		return c.cmdUpdate(ctx)
	case "quit", "exit", "q":
		fmt.Println("Shutting down Energizer...")
		c.eventBus.Emit(ctx, events.Event{
			Type:   events.EventShutdown,
			Source: "cli",
		})
	default:
		fmt.Printf("Unknown command: '%s'. Type 'help' for available commands.\n", cmd)
	}
	return nil
}

// printHelp displays available commands.
func (c *CLI) printHelp() {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Energizer CLI Commands                    ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  status [port]      Show status of all or specific server   ║")
	fmt.Println("║  shutdown <port>    Stop a game server                      ║")
	fmt.Println("║  startup <port>     Start a game server                     ║")
	fmt.Println("║  wake <port>        Wake a sleeping server                  ║")
	fmt.Println("║  sleep <port>       Put a server to sleep                   ║")
	fmt.Println("║  message <port> msg Send in-game message                    ║")
	fmt.Println("║  addservers <n>     Add N new server instances              ║")
	fmt.Println("║  reconnect          Reconnect to upstream services          ║")
	fmt.Println("║  setconfig <k> <v>  Update a configuration value            ║")
	fmt.Println("║  update             Check for updates                       ║")
	fmt.Println("║  quit               Shutdown Energizer                      ║")
	fmt.Println("║  help               Show this help message                  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

// printStatus displays server status in a formatted table.
func (c *CLI) printStatus(args []string) {
	instances := c.manager.GetAllInfo()

	if len(args) > 0 {
		// Show specific server
		port, err := strconv.ParseUint(args[0], 10, 16)
		if err != nil {
			fmt.Println("Invalid port number")
			return
		}

		for _, inst := range instances {
			if inst.Port == uint16(port) {
				c.printServerDetail(inst)
				return
			}
		}
		fmt.Printf("Server not found on port %d\n", port)
		return
	}

	// Show all servers
	fmt.Println()

	tw := tablewriter.NewWriter(os.Stdout)
	tw.SetHeader([]string{"Port", "Status", "Phase", "Players", "Match ID", "Uptime", "PID"})
	tw.SetBorder(true)
	tw.SetAutoWrapText(false)

	for _, inst := range instances {
		status := inst.State.Status.String()
		phase := inst.State.Phase.String()
		uptime := inst.Uptime
		pid := fmt.Sprintf("%d", inst.PID)

		if !inst.Running {
			status = "STOPPED"
			uptime = "-"
			pid = "-"
		}

		if !inst.Enabled {
			status = "DISABLED"
		}

		tw.Append([]string{
			fmt.Sprintf("%d", inst.Port),
			status,
			phase,
			fmt.Sprintf("%d", inst.State.PlayerCount),
			fmt.Sprintf("%d", inst.State.MatchID),
			uptime,
			pid,
		})
	}

	tw.Render()
	fmt.Println()
}

// printServerDetail prints detailed info for a single server.
func (c *CLI) printServerDetail(inst server.InstanceInfo) {
	fmt.Printf("\n  Server Port:  %d\n", inst.Port)
	fmt.Printf("  Status:       %s\n", inst.State.Status)
	fmt.Printf("  Phase:        %s\n", inst.State.Phase)
	fmt.Printf("  Enabled:      %v\n", inst.Enabled)
	fmt.Printf("  Running:      %v\n", inst.Running)
	fmt.Printf("  PID:          %d\n", inst.PID)
	fmt.Printf("  Uptime:       %s\n", inst.Uptime)
	fmt.Printf("  Match ID:     %d\n", inst.State.MatchID)
	fmt.Printf("  Map:          %s\n", inst.State.MapName)
	fmt.Printf("  Players:      %d\n", inst.State.PlayerCount)
	fmt.Printf("  CPU Usage:    %.1f%%\n", inst.State.CPUUsage)
	fmt.Printf("  Lag Events:   %d\n", inst.State.TotalLagEvents)
	fmt.Printf("  Next Restart: %s\n", inst.NextRestart.Format(time.RFC3339))

	if len(inst.State.Players) > 0 {
		fmt.Println("  Players:")
		for name := range inst.State.Players {
			fmt.Printf("    - %s\n", name)
		}
	}
	fmt.Println()
}

func (c *CLI) cmdShutdown(ctx context.Context, args []string) error {
	port, err := parsePortArg(args)
	if err != nil {
		return err
	}

	c.eventBus.Emit(ctx, events.Event{
		Type:   events.EventShutdownServer,
		Source: "cli",
		Payload: events.ServerCommandPayload{Port: uint16(port)},
	})
	fmt.Printf("Shutdown command sent to port %d\n", port)
	return nil
}

func (c *CLI) cmdWake(ctx context.Context, args []string) error {
	port, err := parsePortArg(args)
	if err != nil {
		return err
	}

	c.eventBus.Emit(ctx, events.Event{
		Type:   events.EventWakeServer,
		Source: "cli",
		Payload: events.ServerCommandPayload{Port: uint16(port)},
	})
	fmt.Printf("Wake command sent to port %d\n", port)
	return nil
}

func (c *CLI) cmdSleep(ctx context.Context, args []string) error {
	port, err := parsePortArg(args)
	if err != nil {
		return err
	}

	c.eventBus.Emit(ctx, events.Event{
		Type:   events.EventSleepServer,
		Source: "cli",
		Payload: events.ServerCommandPayload{Port: uint16(port)},
	})
	fmt.Printf("Sleep command sent to port %d\n", port)
	return nil
}

func (c *CLI) cmdMessage(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: message <port> <message>")
	}

	port, err := strconv.ParseUint(args[0], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid port: %s", args[0])
	}

	message := strings.Join(args[1:], " ")
	c.eventBus.Emit(ctx, events.Event{
		Type:   events.EventMessageServer,
		Source: "cli",
		Payload: events.ServerCommandPayload{
			Port: uint16(port),
			Args: []string{message},
		},
	})
	fmt.Printf("Message sent to port %d: %s\n", port, message)
	return nil
}

func (c *CLI) cmdStartup(ctx context.Context, args []string) error {
	port, err := parsePortArg(args)
	if err != nil {
		return err
	}

	inst, ok := c.manager.GetInstance(uint16(port))
	if !ok {
		return fmt.Errorf("server not found on port %d", port)
	}

	if err := inst.Start(ctx); err != nil {
		return err
	}
	fmt.Printf("Server started on port %d\n", port)
	return nil
}

func (c *CLI) cmdAddServers(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: addservers <count>")
	}

	count, err := strconv.Atoi(args[0])
	if err != nil || count < 1 {
		return fmt.Errorf("invalid count: %s", args[0])
	}

	if err := c.manager.AddServers(ctx, count); err != nil {
		return err
	}
	fmt.Printf("Added %d servers\n", count)
	return nil
}

func (c *CLI) cmdReconnect(ctx context.Context) error {
	c.eventBus.Emit(ctx, events.Event{
		Type:   events.EventAuthenticateChat,
		Source: "cli",
	})
	fmt.Println("Reconnection initiated")
	return nil
}

func (c *CLI) cmdSetConfig(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: setconfig <key> <value>")
	}

	key := args[0]
	value := strings.Join(args[1:], " ")

	if err := c.cfg.UpdateHoNField(key, value); err != nil {
		return err
	}

	if err := c.cfg.Save(); err != nil {
		return err
	}

	fmt.Printf("Config updated: %s = %s\n", key, value)
	return nil
}

func (c *CLI) cmdUpdate(ctx context.Context) error {
	c.eventBus.Emit(ctx, events.Event{
		Type:   events.EventUpdate,
		Source: "cli",
	})
	fmt.Println("Update check initiated")
	return nil
}

func parsePortArg(args []string) (uint64, error) {
	if len(args) < 1 {
		return 0, fmt.Errorf("port number required")
	}
	port, err := strconv.ParseUint(args[0], 10, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid port: %s", args[0])
	}
	return port, nil
}

// writerAdapter adapts fmt.Printf to io.Writer for tablewriter.
type writerAdapter struct{}

func (w writerAdapter) Write(p []byte) (n int, err error) {
	fmt.Print(string(p))
	return len(p), nil
}

// lineReader is a simple cross-platform line reader.
type lineReader struct {
	// Implementation uses bufio.Scanner for basic input
	scanner interface{ Scan() bool; Text() string }
	closer  io.Closer
}

func newLineReader() *lineReader {
	return &lineReader{}
}

func (lr *lineReader) ReadLine(prompt string) (string, error) {
	fmt.Print(prompt)
	var line string
	_, err := fmt.Scanln(&line)
	return line, err
}

func (lr *lineReader) Close() error {
	if lr.closer != nil {
		return lr.closer.Close()
	}
	return nil
}
