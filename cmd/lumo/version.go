package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/FeiBaiKin/lumo/internal/version"
)

// runVersion 输出版本信息，支持 -json 便于脚本消费。
func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "以 JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	info := version.Get()
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(info); err != nil {
			return fmt.Errorf("输出版本信息: %w", err)
		}
		return nil
	}
	fmt.Println(info)
	return nil
}
