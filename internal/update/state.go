package update

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// stateFileName 是升级状态文件，落在工作目录根下。
//
// 放 data/ 而不是程序目录：容器里正是程序目录会被重建回去，而 data/ 是挂载卷，
// 「上次装到哪个版本」这件事必须活得比容器长——否则回退检测恰好在最需要它的
// 那一种部署上失效。
const stateFileName = "update-state.json"

// state 是升级留下的痕迹。
type state struct {
	// InstalledVersion 是最近一次**由在线升级**装上的版本。
	InstalledVersion string `json:"installedVersion"`
	// InstalledAt 是那次升级完成的时刻。
	InstalledAt time.Time `json:"installedAt"`
}

// Rollback 描述一次被检测到的版本回退。
type Rollback struct {
	// From 是在线升级装过的版本，也就是本该在跑的那个。
	From string `json:"from"`
	// To 是现在实际在跑的版本。
	To string `json:"to"`
	// At 是那次升级的时刻，用来让站长对上「我那天确实升过级」。
	At time.Time `json:"at"`
}

// readState 读取状态文件。文件不存在或读坏了都返回零值：
// 这份记录是锦上添花的诊断信息，没有它功能照常。
func readState(dataDir string) state {
	data, err := os.ReadFile(filepath.Join(dataDir, stateFileName))
	if err != nil {
		return state{}
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return state{}
	}
	return st
}

// writeState 记下这次升级装上了什么。
func writeState(dataDir, version string) error {
	payload, err := json.MarshalIndent(state{
		InstalledVersion: version,
		InstalledAt:      time.Now(),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, stateFileName), payload, 0o600)
}

// detectRollback 判断当前跑的版本是不是比在线升级装过的那个旧。
//
// 这是容器里允许就地升级的前提：换掉的二进制活在可写层，重建容器就没了，
// 而站长不会把「上个月改过端口」和「版本怎么退回去了」联系起来。
// 有了它，回退至少是**说出来的**——日志里一条 warn，后台关于页一条提示。
//
// 主动降级也会命中，这是对的：它陈述的就是事实，而再升一次就自动消失。
func detectRollback(dataDir, currentVersion string) *Rollback {
	st := readState(dataDir)
	if st.InstalledVersion == "" {
		return nil
	}
	installed, ok := ParseVersion(st.InstalledVersion)
	if !ok {
		return nil
	}
	current, ok := ParseVersion(currentVersion)
	if !ok {
		return nil
	}
	if CompareVersions(current, installed) >= 0 {
		return nil
	}
	return &Rollback{From: installed.Raw, To: current.Raw, At: st.InstalledAt}
}
