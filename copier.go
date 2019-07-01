package copier

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
)

// defaultCopier copier without tag key
var defaultCopier = NewCopier("", "")

// Copy copy things
func Copy(toValue interface{}, fromValue interface{}) (err error) {
	return defaultCopier.Copy(toValue, fromValue)
}

// Copier copier with tag
type Copier struct {
	toTagKey, fromTagKey string
}

// NewCopier new copier
func NewCopier(toTagKey, fromTagKey string) *Copier {
	return &Copier{
		toTagKey:   toTagKey,
		fromTagKey: fromTagKey,
	}
}

// Copy copy things with tag key
func (copier *Copier) Copy(toValue interface{}, fromValue interface{}) (err error) {
	var (
		isSlice bool
		amount  = 1
		from    = indirect(reflect.ValueOf(fromValue))
		to      = indirect(reflect.ValueOf(toValue))
	)

	if !to.CanAddr() {
		return errors.New("copy to value is unaddressable")
	}

	// Return is from value is invalid
	if !from.IsValid() {
		return
	}

	fromType := indirectType(from.Type())
	toType := indirectType(to.Type())

	// Just set it if possible to assign
	// And need to do copy anyway if the type is struct
	if fromType.Kind() != reflect.Struct && from.Type().AssignableTo(to.Type()) {
		to.Set(from)
		return
	}

	if fromType.Kind() != reflect.Struct || toType.Kind() != reflect.Struct {
		return
	}

	if to.Kind() == reflect.Slice {
		isSlice = true
		if from.Kind() == reflect.Slice {
			amount = from.Len()
		}
	}

	for i := 0; i < amount; i++ {
		var dest, source reflect.Value

		if isSlice {
			// source
			if from.Kind() == reflect.Slice {
				source = indirect(from.Index(i))
			} else {
				source = indirect(from)
			}
			// dest
			dest = indirect(reflect.New(toType).Elem())
		} else {
			source = indirect(from)
			dest = indirect(to)
		}

		// check source
		if source.IsValid() {
			fromTypeFields := deepFields(copier.fromTagKey, fromType)
			// log.Println(fromTypeFields)
			toTypeFields := deepFields(copier.toTagKey, toType)
			// log.Println(toTypeFields)

			// Copy from field to field or method
			for name, field := range fromTypeFields {
				if fromField := source.FieldByName(field.Name); fromField.IsValid() {
					// has field
					if toTypeField, exist := toTypeFields[name]; exist {
						if toField := dest.FieldByName(toTypeField.Name); toField.IsValid() {
							if toField.CanSet() {
								if !set(toField, fromField) {
									if err := copier.Copy(toField.Addr().Interface(), fromField.Interface()); err != nil {
										return err
									}
								}
							}
							continue
						}
					}

					// try to set to method
					var toMethod reflect.Value
					if dest.CanAddr() {
						toMethod = dest.Addr().MethodByName(name)
					} else {
						toMethod = dest.MethodByName(name)
					}

					if toMethod.IsValid() && toMethod.Type().NumIn() == 1 && fromField.Type().AssignableTo(toMethod.Type().In(0)) {
						toMethod.Call([]reflect.Value{fromField})
					}
				}
			}

			// Copy from method to field
			for name, toTypefield := range toTypeFields {
				var fromMethod reflect.Value
				if source.CanAddr() {
					fromMethod = source.Addr().MethodByName(name)
				} else {
					fromMethod = source.MethodByName(name)
				}

				if fromMethod.IsValid() && fromMethod.Type().NumIn() == 0 && fromMethod.Type().NumOut() == 1 {
					if toField := dest.FieldByName(toTypefield.Name); toField.IsValid() && toField.CanSet() {
						values := fromMethod.Call([]reflect.Value{})
						if len(values) >= 1 {
							set(toField, values[0])
						}
					}
				}
			}
		}
		if isSlice {
			if dest.Addr().Type().AssignableTo(to.Type().Elem()) {
				to.Set(reflect.Append(to, dest.Addr()))
			} else if dest.Type().AssignableTo(to.Type().Elem()) {
				to.Set(reflect.Append(to, dest))
			}
		}
	}
	return
}

func deepFields(tagKey string, reflectType reflect.Type) map[string]reflect.StructField {
	fields := make(map[string]reflect.StructField)

	if reflectType = indirectType(reflectType); reflectType.Kind() == reflect.Struct {
		for i := 0; i < reflectType.NumField(); i++ {
			v := reflectType.Field(i)
			if v.Anonymous {
				anonymous := deepFields(tagKey, v.Type)
				for name, field := range anonymous {
					if _, exist := fields[name]; !exist {
						fields[name] = field
					}
				}
			} else {
				name, skip := fieldName(tagKey, v)
				if skip {
					continue
				}
				fields[name] = v
			}
		}
	}

	return fields
}

func fieldName(tagKey string, field reflect.StructField) (name string, skip bool) {
	tagParts := strings.Split(tagKey, ".")
	tagKey = tagParts[0]
	tagField := ""
	if len(tagParts) >= 2 {
		tagField = tagParts[1]
	}

	if tagKey == "" {
		name = field.Name
	} else {
		key := field.Tag.Get(tagKey)
		key = strings.ReplaceAll(key, " ", "")
		if key == "" {
			name = field.Name
		} else if key == "-" {
			skip = true
		} else if tagField == "" {
			keys := strings.Split(key, ",")
			name = keys[0]
		} else {
			keys := strings.Split(key, ",")
			for _, key := range keys {
				pair := strings.Split(key, "=")
				if len(pair) == 2 && pair[0] == tagField {
					name = pair[1]
				}
			}
		}
	}
	return
}

func indirect(reflectValue reflect.Value) reflect.Value {
	for reflectValue.Kind() == reflect.Ptr {
		reflectValue = reflectValue.Elem()
	}
	return reflectValue
}

func indirectType(reflectType reflect.Type) reflect.Type {
	for reflectType.Kind() == reflect.Ptr || reflectType.Kind() == reflect.Slice {
		reflectType = reflectType.Elem()
	}
	return reflectType
}

func set(to, from reflect.Value) bool {
	if from.IsValid() {
		if to.Kind() == reflect.Ptr {
			//set `to` to nil if from is nil
			if from.Kind() == reflect.Ptr && from.IsNil() {
				to.Set(reflect.Zero(to.Type()))
				return true
			} else if to.IsNil() {
				to.Set(reflect.New(to.Type().Elem()))
			}
			to = to.Elem()
		}

		if from.Type().ConvertibleTo(to.Type()) {
			to.Set(from.Convert(to.Type()))
		} else if scanner, ok := to.Addr().Interface().(sql.Scanner); ok {
			err := scanner.Scan(from.Interface())
			if err != nil {
				return false
			}
		} else if from.Kind() == reflect.Ptr {
			return set(to, from.Elem())
		} else {
			return false
		}
	}
	return true
}
