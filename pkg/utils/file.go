package utils

import (
	"NetherLink-server/config"
	"fmt"
	"os"
	"path/filepath"
)

// GetExecDir 获取可执行文件所在目录
func GetExecDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

// GetImageSavePath 获取图片保存的绝对路径（基于当前工作目录）
func GetImageSavePath(filename string) string {
	uploadDir := config.GlobalConfig.Image.UploadDir
	return filepath.Join(uploadDir, filename)
}

// GetImageURL 获取图片的HTTP访问URL
func GetImageURL(filename string) string {
	return config.GlobalConfig.Image.URLPrefix + "/" + filename
}

// GetFullImageURL 获取完整图片URL（带协议和host）
func GetFullImageURL(filename string) string {
	BaseUrl := config.GlobalConfig.Server.HTTP.BaseURL
	return fmt.Sprintf("%s%s/%s", BaseUrl, config.GlobalConfig.Image.URLPrefix, filename)
}
