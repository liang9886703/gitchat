package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Chatbot struct {
	BaseURL   string `mapstructure:"baseURL"`
	Model     string `mapstructure:"model"`
	MaxTokens int    `mapstructure:"maxTokens"`
	IsStream  bool   `mapstructure:"isStream"`
	APIKey    string
}

type Namespace struct {
	Name      string  `mapstructure:"name"`
	ChatbotID string  `mapstructure:"chatbotId"`
	Chatbot   Chatbot `mapstructure:"chatbot"`
}
type Template struct {
	Sysprompt      string
	Userprompt     string
	Tools          []string
	Responseformat string
}
type Chattemplate struct {
	Name     string   `mapstructure:"name"`
	Template Template `mapstructure:"template"`
}

type Config struct {
	Namespace    []Namespace    `mapstructure:"namespace"`
	Chattemplate []Chattemplate `mapstructure:"chattemplate"`
	Others       string         `mapstructure:"others"`
}

func GetConfig() (*Config, error) {
	var err error
	cfg := viper.New() // 提到不建议使用全局实例
	cfg.SetConfigName("config")
	cfg.SetConfigType("yaml")
	cfg.AddConfigPath("/work/config/")

	//cfg.SetDefault("ContentDir", "content")

	if err = cfg.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config

	if err := cfg.Unmarshal(&config); err != nil {
		return nil, err
	}

	for i := range config.Namespace {
		apiKeyEnv := fmt.Sprintf("OPENAI_API_KEY_%s", config.Namespace[i].ChatbotID)
		if os.Getenv(apiKeyEnv) == "" {
			fmt.Printf("需要设置 OPENAI_API_KEY 环境变量: %s\n", apiKeyEnv)
			return nil, errors.New("需要设置 OPENAI_API_KEY 环境变量")
		}
		config.Namespace[i].Chatbot.APIKey = os.Getenv(apiKeyEnv)
		//fmt.Println(apiKeyEnv, config.Namespace[i].Chatbot.APIKey)
	}
	// namespacesViper := cfg.Sub("namespace")
	// if namespacesViper == nil {
	// 	return nil, errors.New("namespace configuration not found")
	// }

	// // namespacesViper是一个列表
	// var found bool
	// for i := 0; ; i++ {
	// 	nsKey := fmt.Sprintf("%d", i)
	// 	nsItem := namespacesViper.Sub(nsKey) //通过索引获取列表中的每个元素
	// 	if nsItem == nil {
	// 		break // 没有更多 namespace
	// 	}
	// 	if !nsItem.IsSet("name") || !nsItem.IsSet("chatbotId") {
	// 		continue
	// 	}

	// 	chatbotCfg := nsItem.Sub("chatbot")
	// 	if chatbotCfg == nil {
	// 		continue
	// 	}
	// 	if config.Chatbot, err = getChatbotConfig(chatbotCfg); err != nil {
	// 		return nil, err
	// 	}
	// 	config.Chatbot.Name = nsItem.GetString("name")
	// 	config.Chatbot.ChatbotId = nsItem.GetString("chatbotId")

	// 	found = true
	// 	break // 只取第一个有 chatbot 的 namespace
	// }

	// if !found {
	// 	return nil, errors.New("no valid chatbot configuration found in any namespace")
	// }

	//viper.Set("Verbose", true)

	return &config, nil
}

// viper支持远程监听etcd地址中的配置文件

/* 监听配置文件变化
viper.OnConfigChange(func(e fsnotify.Event) {
	fmt.Println("Config file changed:", e.Name)
})
viper.WatchConfig()
*/
