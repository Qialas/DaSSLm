package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// legacyDefaultJWTSecret 旧版示例配置中公开的默认密钥，视同未配置
const legacyDefaultJWTSecret = "please-change-this-secret"

// ensureJWTSecret 保证 JWT 密钥可用：配置非空则直接复用；
// 为空（或仍是公开的旧默认值）时自动生成随机密钥，并写回配置文件，重启后继续复用。
// 写回失败时仅在内存中使用新密钥，此时每次重启都需要重新登录。
func ensureJWTSecret(cfg *Config, path string, raw []byte) error {
	secret := strings.TrimSpace(cfg.JWT.Secret)
	if secret != "" && secret != legacyDefaultJWTSecret {
		cfg.JWT.Secret = secret
		if len(secret) < 32 {
			log.Printf("警告：jwt.secret 长度不足 32 个字符，建议清空后由程序自动生成")
		}
		return nil
	}
	if secret == legacyDefaultJWTSecret {
		log.Printf("检测到 jwt.secret 仍为公开的默认值，已自动替换为随机密钥，所有用户需要重新登录")
	}

	generated, err := randomSecret()
	if err != nil {
		return fmt.Errorf("生成 JWT 密钥失败：%w", err)
	}
	cfg.JWT.Secret = generated

	updated, err := withJWTSecret(raw, generated)
	if err == nil {
		err = writeFileKeepMode(path, updated)
	}
	if err != nil {
		log.Printf("警告：JWT 密钥无法写回配置文件 %s（%v），本次使用临时密钥，重启后需要重新登录", path, err)
		return nil
	}
	log.Printf("已自动生成 JWT 密钥并写入 %s", path)
	return nil
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// withJWTSecret 在原始 YAML 文本中只改写 jwt.secret 这一项，保留其余内容、注释和格式。
func withJWTSecret(raw []byte, secret string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	value := strconv.Quote(secret)
	lines := strings.Split(string(raw), "\n")

	var root *yaml.Node
	if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		root = doc.Content[0]
	}
	jwtKey, jwtNode := mappingEntry(root, "jwt")

	switch {
	case jwtNode != nil && jwtNode.Kind == yaml.MappingNode:
		if _, secretNode := mappingEntry(jwtNode, "secret"); secretNode != nil {
			// 替换已有的 secret 值，保留行内注释
			idx := secretNode.Line - 1
			line := lines[idx]
			prefix := line[:secretNode.Column-1]
			replacement := prefix + value
			if secretNode.LineComment != "" {
				replacement += " " + secretNode.LineComment
			}
			lines[idx] = replacement
		} else {
			// jwt 段存在但缺少 secret：插入到 jwt 段第一行之前
			first := jwtNode.Content[0]
			indent := strings.Repeat(" ", first.Column-1)
			idx := first.Line - 1
			lines = append(lines[:idx], append([]string{indent + "secret: " + value}, lines[idx:]...)...)
		}
	case jwtKey != nil:
		// jwt: 为空值，在其后补一行
		idx := jwtKey.Line - 1
		lines[idx] = strings.TrimRight(lines[idx][:jwtKey.Column-1]+"jwt:", " ")
		lines = append(lines[:idx+1], append([]string{"  secret: " + value}, lines[idx+1:]...)...)
	default:
		text := strings.TrimRight(string(raw), "\n")
		if text != "" {
			text += "\n\n"
		}
		return []byte(text + "jwt:\n  secret: " + value + "\n"), nil
	}

	result := []byte(strings.Join(lines, "\n"))
	// 校验写回结果仍是合法配置且密钥正确
	var check Config
	if err := yaml.Unmarshal(result, &check); err != nil || check.JWT.Secret != secret {
		return nil, fmt.Errorf("写回配置校验失败")
	}
	return result, nil
}

func mappingEntry(node *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

func writeFileKeepMode(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	return os.WriteFile(path, data, info.Mode().Perm())
}
