// Command lumo 是 Lumo CMS 的唯一入口。
//
// 用法：lumo serve | migrate | admin | version
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	// 内嵌 IANA 时区库（约 450 KB）。
	//
	// 站点设置要求填写真实存在的 IANA 时区名，而 time.LoadLocation 默认要么读系统
	// 时区库（Windows 上没有），要么读 $GOROOT/lib/time/zoneinfo.zip——发布用的
	// -trimpath 构建连 GOROOT 都不再嵌入，装不了 Go 的机器上一律解析失败。
	// 目标是「单一静态二进制到哪都一样」，时区库必须跟着走。
	_ "time/tzdata"
)

const usage = `Lumo — 用 Go 编写的现代化开源 CMS

用法：
  lumo <命令> [参数]

命令：
  serve       启动 HTTP 服务
  migrate     管理数据库迁移（up / down / status / version）
  admin       管理用户（create-user / reset-password / list-users）
  version     输出版本信息

用 "lumo <命令> -h" 查看具体命令的参数。

环境变量：
  LUMO_DATABASE_DSN      数据库连接串（必需，口令不写入配置文件）
  LUMO_ADDR              监听地址，默认 :8080
  LUMO_SECURE_COOKIES    会话 Cookie 是否启用 Secure，生产环境应为 true
  LUMO_TRUSTED_PROXIES   可信反向代理 CIDR，逗号分隔；留空则忽略 X-Forwarded-For
  LUMO_LOG_LEVEL         日志级别 debug/info/warn/error
  LUMO_LOG_FORMAT        日志格式 text/json
  LUMO_DATA_DIR          工作目录，默认 ./data
`

// 复用的字面量，避免同一字符串在多处硬编码后不同步。
const (
	keyStatus  = "status"
	keyVersion = "version"
	cmdUp      = "up"
	cmdDown    = "down"
)

func main() {
	// 退出码集中在 main 处理，便于各子命令内部统一用 error 返回。
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "lumo: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("缺少命令")
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "admin":
		return runAdmin(args[1:])
	case keyVersion:
		return runVersion(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("未知命令 %q", args[0])
	}
}
