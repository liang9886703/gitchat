package main

import (
	"fmt"
	"reflect"
	"testing"

	"gitchat/src/config"
)

func TestGetConfig(t *testing.T) {
	config, err := config.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}
	fmt.Printf("Config: %+v\n", config)
	checkFieldsNonZero(t, reflect.ValueOf(config), "Config")
}

// 判断结构体所有字段是否有零值，递归处理嵌套结构体
func checkFieldsNonZero(t *testing.T, val reflect.Value, path string) {
	// 这个函数的问题是如果配置文件的值和变量的默认值一样，会被误识别为没有赋值
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := val.Type().Field(i)
		fieldName := fieldType.Name
		fullPath := fieldName
		if path != "" {
			fullPath = path + "." + fieldName
		}
		// 跳过config结构体中不可导出字段，即首字母小写的字段
		if !field.CanInterface() {
			fmt.Println("skip key:", field.Kind(), fullPath)
			continue
		}
		switch field.Kind() {
		case reflect.Struct:
			checkFieldsNonZero(t, field, fullPath)
		case reflect.Bool:
			continue
		case reflect.Slice:
			for j := 0; j < field.Len(); j++ {
				checkFieldsNonZero(t, field.Index(j), fullPath)
			}
		default:
			zero := reflect.Zero(field.Type()).Interface()
			fmt.Println(field.Kind(), fullPath, field.Interface())
			if reflect.DeepEqual(field.Interface(), zero) {
				t.Errorf("Field %s is zero value", fullPath)
			}
		}
	}
}
