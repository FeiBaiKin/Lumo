// Package secret 保管那些「必须长期留在库里、又不能明文躺着」的凭据（agent.md §9）。
//
// 背景：SMTP 口令与 S3 访问密钥此前只从环境变量读。那条规则挡住了口令，也把站长
// 挡在了 SSH 里——改一次口令要登服务器、改环境、重启进程。2026-09-14 起改为后台可配，
// 但「不得随备份与接口响应流出」这条底线要由本包接着守住：
//
//   - 库里存的是密文，主密钥在工作目录的 secret.key 里（0600）。数据库备份单独泄漏
//     解不开任何东西，要连密钥文件一起拿到才行；
//   - 密文带 enc:v1: 前缀，版本号留在前缀里，日后换算法可以新旧并存；
//   - 不带前缀的值按明文读。这条不是宽容，是升级路径：本包之前手工写进库的值
//     不会因为加了个前缀就突然读不出来。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvKey 是指定主密钥的环境变量名（base64 编码的 32 字节）。
	//
	// 设置后优先于密钥文件：多实例部署若各自只有本地磁盘，必须靠它共用同一把密钥，
	// 否则每个实例各生成一把，A 存下的口令 B 解不开。
	EnvKey = "LUMO_SECRET_KEY"

	// KeyFileName 是主密钥文件名，位于工作目录（dataDir）下。
	KeyFileName = "secret.key"

	// keySize 是 AES-256 的密钥长度。
	keySize = 32

	// prefix 标记一个值是密文。
	prefix = "enc:v1:"
)

// ErrKeyMismatch 表示密文与当前密钥不匹配——密钥文件丢了、换了，或来自另一套部署。
//
// 调用方不应把它当成致命错误：解不开的口令按「未设置」处理并让站长重填一次，
// 总好过把整个设置页打成 500——那样他连填回来的入口都没有。
var ErrKeyMismatch = errors.New("密文与当前主密钥不匹配")

// Keyring 是一把已加载的主密钥。
//
// 它是只读的：一把密钥一个实例，加载后不再替换。运行期轮换密钥需要重新加密
// 全部已存凭据，那是另一个需求，真要做时应当做成显式的迁移而不是悄悄换掉。
type Keyring struct {
	aead cipher.AEAD
}

// New 用给定密钥构造 Keyring。
func New(key []byte) (*Keyring, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secret: 主密钥须为 %d 字节，实际 %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: 构造 AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: 构造 GCM: %w", err)
	}
	return &Keyring{aead: aead}, nil
}

// Open 加载主密钥：环境变量优先，其次工作目录下的 secret.key，都没有就生成一把。
//
// 生成的密钥只有 0600 的密钥文件一个副本，丢了已存的口令就解不回来——
// 所以它必须与 data/ 一起备份，运维文档里写死了这一条。
func Open(dataDir string) (*Keyring, error) {
	if raw := strings.TrimSpace(os.Getenv(EnvKey)); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("secret: %s 不是合法的 base64（用 openssl rand -base64 32 生成）: %w", EnvKey, err)
		}
		return New(key)
	}

	path := filepath.Join(dataDir, KeyFileName)
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if decodeErr != nil {
			return nil, fmt.Errorf("secret: %s 内容不是合法的 base64: %w", path, decodeErr)
		}
		// 权限过宽就地收紧。密钥文件是 0644 时没有任何报错，但任何能读工作目录的
		// 进程都能拿走它——这种「不会响的错」只能由读到它的人顺手修掉。
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(path, 0o600)
		}
		return New(key)
	case errors.Is(err, fs.ErrNotExist):
		return create(path)
	default:
		return nil, fmt.Errorf("secret: 读取 %s: %w", path, err)
	}
}

// create 生成一把新密钥并落盘。
func create(path string) (*Keyring, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secret: 生成主密钥: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("secret: 创建工作目录: %w", err)
	}
	// 用 O_EXCL 创建：两个实例同时首启时，只有一个能建成，另一个会去读已建好的那把。
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Open(filepath.Dir(path))
		}
		return nil, fmt.Errorf("secret: 创建 %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		return nil, fmt.Errorf("secret: 写入 %s: %w", path, err)
	}
	return New(key)
}

// Seal 加密一个明文值。空串原样返回：空串的语义就是「没设置」，
// 把它编成一段密文反倒会让它变成「已设置」。
func (k *Keyring) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secret: 生成随机数: %w", err)
	}
	// 随机 nonce 前置在密文之前。GCM 的 nonce 不要求保密，只要求不重复。
	sealed := k.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Unseal 解密一个已存值。不带前缀的值按明文原样返回（升级路径，见包注释）。
func (k *Keyring) Unseal(stored string) (string, error) {
	if !Encrypted(stored) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", fmt.Errorf("%w: 密文不是合法的 base64", ErrKeyMismatch)
	}
	// GCM 的认证失败就是「解开之后对不上」，nonce 长度不足也归到同一类，
	// 免得调用方要分辨两种其实同样无解的情况。
	size := k.aead.NonceSize()
	if len(raw) < size {
		return "", ErrKeyMismatch
	}
	plain, err := k.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", ErrKeyMismatch
	}
	return string(plain), nil
}

// Encrypted 判断一个已存值是不是密文。
func Encrypted(stored string) bool { return strings.HasPrefix(stored, prefix) }
