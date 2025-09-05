package xviper

import (
	"fmt"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"

	"github.com/spf13/viper"
)

var (
	current  *config
	pipeline chan command
)

func init() {
	current = &config{
		Loaded:    false,
		Filename:  "",
		Timestamp: time.Now(),
		Viper:     viper.New(),
	}
	pipeline = make(chan command)
	go runner(pipeline)
}

type config struct {
	Loaded    bool
	Filename  string
	Lockfile  string
	Timestamp time.Time
	Viper     *viper.Viper
}

func (it *config) Save() {
	if common.WarrantyVoided() {
		return
	}
	if len(it.Filename) == 0 {
		return
	}
	completed := pathlib.LockWaitMessage(it.Lockfile, "Serialized config access [config lock]")
	locker, err := pathlib.Locker(it.Lockfile, 125, false)
	completed()
	if err != nil {
		common.Log("FATAL: could not lock %v, reason %v; ignored.", it.Lockfile, err)
		return
	}
	defer locker.Release()

	err = it.Viper.WriteConfigAs(it.Filename)
	if err != nil {
		common.Log("FATAL: could not write %v, reason %v; ignored.", it.Filename, err)
		return
	}
	defer pathlib.RestrictOwnerOnly(it.Filename)
	when, err := pathlib.Modtime(it.Filename)
	if err == nil {
		it.Timestamp = when
	}
}

func (it *config) reload() {
	completed := pathlib.LockWaitMessage(it.Lockfile, "Serialized config access [config lock]")
	locker, err := pathlib.Locker(it.Lockfile, 125, false)
	completed()
	if err != nil {
		common.Log("FATAL: could not lock %v, reason %v; ignored.", it.Lockfile, err)
		return
	}
	defer locker.Release()

	it.Viper = viper.New()
	it.Viper.SetConfigFile(it.Filename)
	defer pathlib.RestrictOwnerOnly(it.Filename)
	err = it.Viper.ReadInConfig()
	var when time.Time
	if err == nil {
		when, err = pathlib.Modtime(it.Filename)
	}
	if err != nil {
		return
	}
	it.Loaded = true
	it.Timestamp = when
}

func (it *config) Reset(filename string) {
	it.Filename = filename
	it.Lockfile = fmt.Sprintf("%s.lck", filename)
	it.reload()
}

func (it *config) Summon() *viper.Viper {
	if !it.Loaded || len(it.Filename) == 0 {
		return it.Viper
	}
	when, err := pathlib.Modtime(it.Filename)
	if err != nil {
		return it.Viper
	}
	if when.After(it.Timestamp) {
		common.Debug("Configuration %v changed, reloading!", it.Filename)
		it.reload()
	}
	return it.Viper
}

type command func(*config)

func runner(todo <-chan command) {
	for task := range todo {
		task(current)
	}
}

func IsAvailable() bool {
	return len(AllKeys()) > 0
}

func SetConfigFile(in string) {
	pipeline <- func(core *config) {
		core.Reset(in)
	}
}

func Set(key string, value interface{}) {
	flow := make(chan bool)
	pipeline <- func(core *config) {
		tool := core.Summon()
		tool.Set(key, value)
		core.Save()
		flow <- true
	}
	<-flow
}

func Lockfile() string {
	flow := make(chan string)
	pipeline <- func(core *config) {
		flow <- core.Lockfile
	}
	return <-flow
}

func ConfigFileUsed() string {
	flow := make(chan string)
	pipeline <- func(core *config) {
		flow <- core.Summon().ConfigFileUsed()
	}
	return <-flow
}

func AllKeys() []string {
	flow := make(chan []string)
	pipeline <- func(core *config) {
		flow <- core.Summon().AllKeys()
	}
	return <-flow
}

func GetBool(key string) bool {
	flow := make(chan bool)
	pipeline <- func(core *config) {
		flow <- core.Summon().GetBool(key)
	}
	return <-flow
}

func GetUint64(key string) uint64 {
	flow := make(chan uint64)
	pipeline <- func(core *config) {
		flow <- core.Summon().GetUint64(key)
	}
	return <-flow
}

func GetInt64(key string) int64 {
	flow := make(chan int64)
	pipeline <- func(core *config) {
		flow <- core.Summon().GetInt64(key)
	}
	return <-flow
}

func GetString(key string) string {
	flow := make(chan string)
	pipeline <- func(core *config) {
		flow <- core.Summon().GetString(key)
	}
	return <-flow
}

func Get(key string) interface{} {
	flow := make(chan interface{})
	pipeline <- func(core *config) {
		flow <- core.Summon().Get(key)
	}
	return <-flow
}
