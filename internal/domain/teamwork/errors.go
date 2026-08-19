package teamwork

import "fmt"

// errMissingField 缺失字段错误。
func errMissingField(field string) error {
	return fmt.Errorf("团队协作: 缺 %s", field)
}
