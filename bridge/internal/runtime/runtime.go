package runtime

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"yzj-bridge/internal/backends"
	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/config"
	"yzj-bridge/internal/dedupe"
	"yzj-bridge/internal/inbound"
	"yzj-bridge/internal/jobs"
	"yzj-bridge/internal/memory"
	"yzj-bridge/internal/orchestrator"
	"yzj-bridge/internal/paths"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/runlock"
	"yzj-bridge/internal/sessions"
	"yzj-bridge/internal/skills"
	"yzj-bridge/internal/webhook"
	"yzj-bridge/internal/ws"
	"yzj-bridge/internal/wssstate"
)

type Runtime struct {
	mu         sync.Mutex
	CfgPath    string
	File       *config.File
	Defaults   map[string]any
	Reg        *registry.Registry
	Store      *sessions.Store
	SkillStore *skills.Store
	Memory     *memory.Service
	Orch       *orchestrator.Orchestrator
	Disp       *inbound.Dispatcher
	Clients    map[string]*ws.Client
	Webhook    *webhook.Server
	enabled    map[string]bool
}

func New(cfgPath string) *Runtime {
	if cfgPath == "" {
		cfgPath = paths.ConfigPath()
	}
	return &Runtime{
		CfgPath: cfgPath,
		Reg:     registry.New(),
		Clients: map[string]*ws.Client{},
		enabled: map[string]bool{},
	}
}

func (r *Runtime) Load(restoreWSS bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := config.LoadFile(r.CfgPath)
	if err != nil {
		return err
	}
	cfgs, err := config.ExpandBots(f)
	if err != nil {
		return err
	}
	r.File = f
	r.Defaults = f.MergedDefaults()
	sessFile, _ := r.Defaults["sessions_file"].(string)
	store, err := sessions.Open(sessFile)
	if err != nil {
		return err
	}
	r.Store = store
	skillsRoot, _ := r.Defaults["skills_dir"].(string)
	if strings.TrimSpace(skillsRoot) == "" {
		skillsRoot = paths.SkillsDir()
	} else {
		skillsRoot = config.ExpandHome(skillsRoot)
	}
	r.SkillStore = skills.NewStore(skillsRoot)
	_ = r.SkillStore.EnsureRoot()
	for _, cfg := range cfgs {
		if err := r.SkillStore.ValidateIDs(cfg.Skills); err != nil {
			log.Printf("warn: bot %s skills: %v (ignored until installed)", cfg.ID, err)
		}
	}
	for _, cfg := range cfgs {
		if strings.TrimSpace(cfg.Workspace) != "" {
			_ = os.MkdirAll(cfg.Workspace, 0o755)
		}
	}

	bots := make([]*bot.Bot, 0, len(cfgs))
	for _, cfg := range cfgs {
		be, err := backends.Create(cfg, store)
		if err != nil {
			return err
		}
		b := &bot.Bot{Config: cfg, Backend: be}
		b.Status.InboundMode = cfg.InboundMode
		bots = append(bots, b)
	}
	r.Reg.Replace(bots)
	memCfg := memory.ParseConfig(r.Defaults).ResolveEngine(r.Defaults)
	if r.Memory == nil {
		r.Memory = memory.NewService(paths.MemoryDir(), memCfg)
	} else {
		r.Memory.SetConfig(memCfg)
		if r.Memory.Sched != nil {
			r.Memory.Sched.Stop()
		}
		r.Memory.Sched = memory.NewScheduler(r.Memory)
	}
	r.Memory.SetDefaults(r.Defaults)
	if memCfg.Enabled {
		r.Memory.Sched.StartIdleTicker()
	}
	r.Orch = &orchestrator.Orchestrator{
		Reg: r.Reg, Store: store, Skills: r.SkillStore, Locks: runlock.New(), Memory: r.Memory,
	}
	r.Disp = &inbound.Dispatcher{
		Reg: r.Reg, Orch: r.Orch, Dedupe: dedupe.New(), Jobs: jobs.New(), Store: store, Memory: r.Memory,
	}

	// teardown old clients
	for _, c := range r.Clients {
		c.Dispose()
	}
	r.Clients = map[string]*ws.Client{}
	stale := intFrom(r.Defaults["stale_ms"], 45000)
	inv := intFrom(r.Defaults["ws_invalid_frame_limit"], 3)
	rec := time.Duration(intFrom(r.Defaults["reconnect_delay_sec"], 5)) * time.Second
	maxd := time.Duration(intFrom(r.Defaults["reconnect_max_delay_sec"], 60)) * time.Second
	hb := time.Duration(intFrom(r.Defaults["heartbeat_interval_sec"], 30)) * time.Second

	for _, b := range bots {
		if !bot.ModeUsesWS(b.Config.InboundMode) {
			continue
		}
		c := &ws.Client{
			Bot: b, Dispatcher: r.Disp, StaleMS: stale, InvalidLim: inv,
			Reconnect: rec, MaxDelay: maxd, Heartbeat: hb,
		}
		r.Clients[b.Config.ID] = c
	}

	if r.Webhook != nil {
		r.Webhook.Stop()
	}
	needWH := false
	for _, b := range bots {
		if bot.ModeUsesWebhook(b.Config.InboundMode) {
			needWH = true
			break
		}
	}
	if needWH {
		host, _ := r.Defaults["webhook_host"].(string)
		port := intFrom(r.Defaults["webhook_port"], 8765)
		r.Webhook = &webhook.Server{Reg: r.Reg, Dispatcher: r.Disp, Host: host, Port: port}
		_ = r.Webhook.Start()
	}

	if restoreWSS {
		r.enabled = wssstate.Load()
		for id, c := range r.Clients {
			en, ok := r.enabled[id]
			if !ok {
				en = true
			}
			c.SetEnabled(en)
			r.enabled[id] = en
		}
		_ = wssstate.Save(r.enabled)
	}
	log.Printf("runtime loaded bots=%d ws_clients=%d", len(bots), len(r.Clients))
	return nil
}

func (r *Runtime) persistEnabled() {
	_ = wssstate.Save(r.enabled)
}

func (r *Runtime) StartAllWSS() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.Clients {
		c.SetEnabled(true)
		r.enabled[id] = true
	}
	r.persistEnabled()
}

func (r *Runtime) StopAllWSS() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.Clients {
		c.SetEnabled(false)
		r.enabled[id] = false
	}
	r.persistEnabled()
}

func (r *Runtime) SetChannelEnabled(id string, en bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.Clients[id]
	if !ok {
		return fmt.Errorf("channel not found: %s", id)
	}
	c.SetEnabled(en)
	r.enabled[id] = en
	r.persistEnabled()
	return nil
}

func (r *Runtime) SetRoleEnabled(roleID string, en bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.Clients {
		if c.Bot.Config.RoleID == roleID {
			c.SetEnabled(en)
			r.enabled[id] = en
		}
	}
	r.persistEnabled()
}

type StatusItem struct {
	ID          string `json:"id"`
	RoleID      string `json:"role_id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Backend     string `json:"backend"`
	InboundMode string `json:"inbound_mode"`
	Connected   bool   `json:"connected"`
	WSEnabled   bool   `json:"ws_enabled"`
	LastError   string `json:"last_error"`
	HasWS       bool   `json:"has_ws"`
}

func (r *Runtime) SnapshotStatus() []StatusItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []StatusItem
	for _, b := range r.Reg.List() {
		st := b.SnapshotStatus()
		_, hasWS := r.Clients[b.Config.ID]
		out = append(out, StatusItem{
			ID: b.Config.ID, RoleID: b.Config.RoleID, Name: b.Config.Name, Group: b.Config.Group,
			Backend: b.Config.Backend, InboundMode: b.Config.InboundMode,
			Connected: st.Connected, WSEnabled: st.WSEnabled, LastError: st.LastError, HasWS: hasWS,
		})
	}
	return out
}

func (r *Runtime) GetRawConfig() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.File == nil {
		return map[string]any{}
	}
	return r.File.Raw()
}

func (r *Runtime) SaveAndReload(raw map[string]any, keepWSS bool) error {
	// 先 ExpandBots 校验，再落盘，避免半残配置写进磁盘后 reload 失败。
	f, err := config.ParseRaw(raw)
	if err != nil {
		return err
	}
	cfgs, err := config.ExpandBots(f)
	if err != nil {
		return err
	}
	skillsRoot, _ := f.MergedDefaults()["skills_dir"].(string)
	if strings.TrimSpace(skillsRoot) == "" {
		skillsRoot = paths.SkillsDir()
	} else {
		skillsRoot = config.ExpandHome(skillsRoot)
	}
	sk := skills.NewStore(skillsRoot)
	for _, cfg := range cfgs {
		if err := sk.ValidateIDs(cfg.Skills); err != nil {
			// Stale skill refs on *other* bots used to block every save. Drop
			// missing ids from the raw config and continue (Load already warns).
			pruned := pruneMissingSkills(raw, cfg, sk)
			if len(pruned) > 0 {
				log.Printf("warn: bot %s removed uninstalled skills: %s", firstNonEmptyLocal(cfg.RoleID, cfg.ID), strings.Join(pruned, ", "))
			}
		}
	}
	if err := config.SaveRaw(r.CfgPath, raw); err != nil {
		return err
	}
	enabled := map[string]bool{}
	if keepWSS {
		r.mu.Lock()
		for k, v := range r.enabled {
			enabled[k] = v
		}
		r.mu.Unlock()
	}
	if err := r.Load(false); err != nil {
		return err
	}
	if keepWSS {
		r.mu.Lock()
		r.enabled = enabled
		for id, c := range r.Clients {
			en, ok := enabled[id]
			if !ok {
				en = true
			}
			c.SetEnabled(en)
			r.enabled[id] = en
		}
		r.persistEnabled()
		r.mu.Unlock()
	}
	return nil
}

func (r *Runtime) ReloadFromDisk(keepWSS bool) error {
	enabled := map[string]bool{}
	if keepWSS {
		r.mu.Lock()
		for k, v := range r.enabled {
			enabled[k] = v
		}
		r.mu.Unlock()
	}
	if err := r.Load(false); err != nil {
		return err
	}
	if keepWSS {
		r.mu.Lock()
		r.enabled = enabled
		for id, c := range r.Clients {
			en, ok := enabled[id]
			if !ok {
				en = true
			}
			c.SetEnabled(en)
			r.enabled[id] = en
		}
		r.persistEnabled()
		r.mu.Unlock()
	} else {
		r.StartAllWSS()
	}
	return nil
}

func (r *Runtime) Shutdown() {
	r.StopAllWSS()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Memory != nil && r.Memory.Sched != nil {
		r.Memory.Sched.Stop()
	}
	if r.Webhook != nil {
		r.Webhook.Stop()
	}
}

func (r *Runtime) ConfigPath() string { return r.CfgPath }
func (r *Runtime) DataDir() string    { return paths.UserDataDir() }

func intFrom(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return def
	}
}

func firstNonEmptyLocal(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// pruneMissingSkills drops skill ids that are not installed from the matching
// bots[] entry in raw. Returns the removed ids (for logging).
func pruneMissingSkills(raw map[string]any, cfg bot.Config, sk *skills.Store) []string {
	bots, ok := raw["bots"].([]any)
	if !ok {
		return nil
	}
	var removed []string
	role := firstNonEmptyLocal(cfg.RoleID, cfg.ID)
	for _, item := range bots {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id != role && id != cfg.ID {
			continue
		}
		list, ok := m["skills"].([]any)
		if !ok || len(list) == 0 {
			continue
		}
		kept := make([]any, 0, len(list))
		for _, x := range list {
			sid, _ := x.(string)
			sid = strings.TrimSpace(sid)
			if sid == "" {
				continue
			}
			if sk.Has(sid) {
				kept = append(kept, sid)
			} else {
				removed = append(removed, sid)
			}
		}
		m["skills"] = kept
	}
	return removed
}
