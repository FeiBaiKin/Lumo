package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
)

// CronJob 是 plugin.yaml 里 spec.cron 的一项：按固定间隔在后台运行的任务。
type CronJob struct {
	// Name 是任务名，对应 SDK 里 OnCron 登记的名字。
	Name string `yaml:"name" json:"name"`
	// Every 是运行间隔，Go 时长写法，如 10m、1h、24h。
	Every       string `yaml:"every" json:"every"`
	Description string `yaml:"description" json:"description"`

	interval time.Duration
}

// Interval 返回解析后的运行间隔。
func (j *CronJob) Interval() time.Duration { return j.interval }

const (
	maxCronJobs     = 10
	minCronInterval = time.Minute
	maxCronInterval = 30 * 24 * time.Hour
	// cronTick 是扫描到期任务的间隔。
	cronTick = 30 * time.Second
	// cronTimeout 是一次任务的时限。
	cronTimeout = 60 * time.Second
)

var cronNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// normalizeCron 校验 spec.cron：名字唯一、间隔在范围内、声明了能力、有后端去跑。
func normalizeCron(jobs []CronJob, caps *Capabilities, hasBackend bool) error {
	if len(jobs) == 0 {
		return nil
	}
	if !hasBackend {
		return fmt.Errorf("%w：定时任务要由后端代码运行，请同时声明 spec.runtime: %s", ErrInvalidPackage, RuntimeWasm)
	}
	if !caps.Cron {
		return fmt.Errorf("%w：声明了 spec.cron，还要声明 capabilities.cron: true", ErrInvalidPackage)
	}
	if len(jobs) > maxCronJobs {
		return fmt.Errorf("%w：定时任务最多 %d 个", ErrInvalidPackage, maxCronJobs)
	}
	seen := map[string]bool{}
	for i := range jobs {
		job := &jobs[i]
		if !cronNamePattern.MatchString(job.Name) || len(job.Name) > 64 {
			return fmt.Errorf("%w：定时任务名 %q 只能用小写字母、数字与连字符", ErrInvalidPackage, job.Name)
		}
		if seen[job.Name] {
			return fmt.Errorf("%w：定时任务 %q 重复", ErrInvalidPackage, job.Name)
		}
		seen[job.Name] = true
		interval, err := time.ParseDuration(job.Every)
		if err != nil {
			return fmt.Errorf("%w：定时任务 %s 的 every %q 不是合法的时长，如 10m、1h", ErrInvalidPackage, job.Name, job.Every)
		}
		if interval < minCronInterval || interval > maxCronInterval {
			return fmt.Errorf("%w：定时任务 %s 的间隔须在 1 分钟到 30 天之间", ErrInvalidPackage, job.Name)
		}
		job.interval = interval
	}
	return nil
}

// cronRunName 是任务在 job_runs 里的名字：多实例靠它认领执行权。
func cronRunName(plugin, job string) string { return "plugin.cron." + plugin + "." + job }

// cronRun 是认领到的一次任务执行。
type cronRun struct {
	plugin string
	job    string
	key    string
}

// runningJobs 记着本实例上还没跑完的任务，同一个任务不叠着跑。
type runningJobs struct {
	mu   sync.Mutex
	keys map[string]bool
}

func (r *runningJobs) tryStart(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys[key] {
		return false
	}
	r.keys[key] = true
	return true
}

func (r *runningJobs) done(key string) {
	r.mu.Lock()
	delete(r.keys, key)
	r.mu.Unlock()
}

// claimDue 认领到期的任务。执行权经 database.ClaimRun 认领：多个实例同时扫到，只有一个拿得到。
func (m *Module) claimDue(ctx context.Context, running *runningJobs) []cronRun {
	var out []cronRun
	for _, loaded := range m.registry.Enabled() {
		if len(loaded.Manifest.Spec.Cron) == 0 || loaded.Granted == nil || !loaded.Granted.Cron {
			continue
		}
		for i := range loaded.Manifest.Spec.Cron {
			job := &loaded.Manifest.Spec.Cron[i]
			key := cronRunName(loaded.ID(), job.Name)
			if !running.tryStart(key) {
				continue
			}
			claimed, err := database.ClaimRun(ctx, m.db, key, job.interval)
			if err != nil || !claimed {
				running.done(key)
				if err != nil && ctx.Err() == nil {
					m.logger.Warn("认领插件定时任务失败", slog.String("plugin", loaded.ID()), slog.Any("error", err))
				}
				continue
			}
			out = append(out, cronRun{plugin: loaded.ID(), job: job.Name, key: key})
		}
	}
	return out
}

// runJob 执行一次任务。
func (m *Module) runJob(ctx context.Context, run cronRun, running *runningJobs) {
	defer running.done(run.key)
	req := wasm.Request{Type: kindCron, Name: run.job}
	if _, err := m.Invoke(ctx, run.plugin, req, cronTimeout); err != nil && !wasm.IsCrash(err) {
		m.logger.Info("插件定时任务没有成功",
			slog.String("plugin", run.plugin), slog.String("job", run.job), slog.Any("error", err))
	}
}

// runCron 定时扫描并执行到期的任务，直到 ctx 取消。
func (m *Module) runCron(ctx context.Context) {
	ticker := time.NewTicker(cronTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, run := range m.claimDue(ctx, m.cronRunning) {
				go m.runJob(ctx, run, m.cronRunning)
			}
		}
	}
}

// RunDueJobs 立即跑一轮到期的定时任务并等它们跑完，返回跑了几个。供测试与运维排查用。
func (m *Module) RunDueJobs(ctx context.Context) int {
	runs := m.claimDue(ctx, m.cronRunning)
	var wg sync.WaitGroup
	for _, run := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.runJob(ctx, run, m.cronRunning)
		}()
	}
	wg.Wait()
	return len(runs)
}
