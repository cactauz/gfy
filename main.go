package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/ini.v1"

	lua "github.com/yuin/gopher-lua"
)

// type -> key -> name string
var locale map[string]map[string]string

func main() {
	start := time.Now()

	L := lua.NewState()
	setupState(L)

	mods := loadMods(L)

	locale = loadLocale(mods)

	recipes := getRecipes(L)

	// for _, r := range recipes {
	// 	fmt.Println(r)
	// }

	fmt.Println("found a total of", len(recipes), "recipes in", time.Since(start))
}

type Recipe struct {
	Name        string
	Ingredients map[string]float64
	Results     map[string]float64
}

func (r *Recipe) String() string {
	ings := ""
	for k, v := range r.Ingredients {
		item, ok := locale["item"][k]
		if !ok {
			item = k
		}
		ings += fmt.Sprintf("\t%s x %g\n", item, v)
	}

	res := ""
	for k, v := range r.Results {
		item, ok := locale["item"][k]
		if !ok {
			item = k
		}

		res += fmt.Sprintf("\t%s x %g\n", item, v)
	}

	recipe, ok := locale["recipe"][r.Name]
	if !ok {
		recipe = r.Name
	}

	return fmt.Sprintf("%s:\n%sresults:\n%s", recipe, ings, res)
}

func setupState(L *lua.LState) {
	str := "package.path = package.path .. \";./factorio-data-master/core/lualib/?.lua\""
	L.DoString(str)

	// defines expected by mods
	rDiff := L.NewTable()
	rDiff.RawSetString("normal", lua.LBool(true))

	diffS := L.NewTable()
	diffS.RawSetString("recipe_difficulty", rDiff)
	diffS.RawSetString("technology_difficulty", rDiff)

	dirs := L.NewTable()
	dirs.RawSetString("north", lua.LNumber(0))
	dirs.RawSetString("east", lua.LNumber(2))
	dirs.RawSetString("south", lua.LNumber(4))
	dirs.RawSetString("west", lua.LNumber(6))

	defines := L.NewTable()
	defines.RawSetString("difficulty_settings", diffS)
	defines.RawSetString("direction", dirs)
	L.SetGlobal("defines", defines)

	// populated during settings loading
	L.SetGlobal("mods", L.NewTable())

	// sets up data and extending it
	err := L.DoFile("./factorio-data-master/core/lualib/dataloader.lua")
	if err != nil {
		fmt.Println("err:", err)
	}
}

func readMods() []*ModInfo {
	mods := make([]*ModInfo, 0)

	fis, err := ioutil.ReadDir("./mods")
	if err != nil {
		fmt.Println("err:", err)
		return nil
	}

	for _, fi := range fis {
		if fi.IsDir() {
			path := fmt.Sprintf("./mods/%s", fi.Name())
			_, err := ioutil.ReadFile(path + "/data.lua")

			if err == nil {
				info := loadModInfo(path)
				mods = append(mods, info)
			}
		}
	}

	return resolveModOrder(mods)
}

func loadMods(L *lua.LState) []*ModInfo {
	core := &ModInfo{
		Name:         "core",
		Path:         "./factorio-data-master/core",
		Dependencies: map[string]bool{},
	}
	loadModData(L, core)

	base := &ModInfo{
		Name:         "base",
		Path:         "./factorio-data-master/base",
		Dependencies: map[string]bool{},
	}
	loadModData(L, base)

	mods := readMods()
	loadModSettings(L, mods)

	err := L.DoFile("./patch.lua")
	if err != nil {
		fmt.Println("err:", err)
	}

	for _, m := range mods {
		loadModData(L, m)
	}

	return mods
}

func resolveModOrder(mods []*ModInfo) []*ModInfo {
	modNames := map[string]bool{
		"base": true,
		"core": true,
	}
	loadedMods := map[string]bool{
		"base": true,
		"core": true,
	}

	for _, m := range mods {
		modNames[m.Name] = true
	}

	// sort
	sort.Slice(mods, func(i, j int) bool {
		return mods[i].Name < mods[j].Name
	})

	// validate deps and remove optional mods that aren't present
	for _, mod := range mods {
		for dep, req := range mod.Dependencies {
			exists := modNames[dep]
			if !exists {
				if req {
					panic(fmt.Sprintf("missing required dependency %s!", dep))
				}

				delete(mod.Dependencies, dep)
			}
		}
	}

	orderedMods := make([]*ModInfo, 0, len(mods))

	// take the sorted list and iterate repeatedly, removing items each
	// pass who's dependencies are all resolved
	for len(mods) > 0 {
		for i := 0; i < len(mods); i++ {
			mod := mods[i]
			allLoaded := true
			for dep := range mod.Dependencies {
				if !loadedMods[dep] {
					allLoaded = false
					break
				}
			}

			if allLoaded {
				orderedMods = append(orderedMods, mod)
				mods = append(mods[:i], mods[i+1:]...)
				loadedMods[mod.Name] = true
			}
		}
	}

	return orderedMods
}

func loadModSettings(L *lua.LState, mods []*ModInfo) {
	settingState := lua.NewState()
	setupState(settingState)

	ms := settingState.GetGlobal("mods").(*lua.LTable)
	gms := L.GetGlobal("mods").(*lua.LTable)
	for _, m := range mods {
		ms.RawSetString(m.Name, lua.LBool(true))
		gms.RawSetString(m.Name, lua.LBool(true))
	}

	for _, m := range mods {
		str := fmt.Sprintf("package.path = package.path .. \";%s/?.lua\"", m.Path)
		err := settingState.DoString(str)
		if err != nil {
			fmt.Println(err)
		}

		settings := m.Path + "/settings.lua"
		if _, err := os.Stat(settings); os.IsNotExist(err) {
			continue
		}

		err = settingState.DoFile(settings)
		if err != nil {
			fmt.Println(err)
		}
	}

	startup := L.NewTable()

	raw := settingState.GetGlobal("data").(*lua.LTable).
		RawGetString("raw").(*lua.LTable)

	str, ok := raw.RawGetString("string-setting").(*lua.LTable)
	if ok {
		str.ForEach(func(k, v lua.LValue) {
			setting := v.(*lua.LTable)

			val := L.NewTable()
			val.RawSetString("value", setting.RawGetString("default_value"))
			startup.RawSetString(string(setting.RawGetString("name").(lua.LString)), val)
		})
	}

	bl, ok := raw.RawGetString("bool-setting").(*lua.LTable)
	if ok {
		bl.ForEach(func(k, v lua.LValue) {
			setting := v.(*lua.LTable)

			val := L.NewTable()
			val.RawSetString("value", setting.RawGetString("default_value"))
			startup.RawSetString(string(setting.RawGetString("name").(lua.LString)), val)
		})
	}

	it, ok := raw.RawGetString("int-setting").(*lua.LTable)
	if ok {
		it.ForEach(func(k, v lua.LValue) {
			setting := v.(*lua.LTable)

			val := L.NewTable()
			val.RawSetString("value", setting.RawGetString("default_value"))
			startup.RawSetString(string(setting.RawGetString("name").(lua.LString)), val)
		})
	}

	db, ok := raw.RawGetString("double-setting").(*lua.LTable)
	if ok {
		db.ForEach(func(k, v lua.LValue) {
			setting := v.(*lua.LTable)

			val := L.NewTable()
			val.RawSetString("value", setting.RawGetString("default_value"))
			startup.RawSetString(string(setting.RawGetString("name").(lua.LString)), val)
		})
	}

	settings := L.NewTable()
	settings.RawSetString("startup", startup)
	L.SetGlobal("settings", settings)
}

type ModInfo struct {
	Name string
	Path string
	// maps name -> is required dependency
	Dependencies map[string]bool
}

func loadModInfo(path string) *ModInfo {
	f, err := ioutil.ReadFile(path + "/info.json")
	if err != nil {
		fmt.Println("err:", err)
		return nil
	}

	info := make(map[string]interface{})
	err = json.Unmarshal(f, &info)
	if err != nil {
		fmt.Println("err:", err)
		return nil
	}

	deps := make(map[string]bool)

	ds, ok := info["dependencies"].([]interface{})
	if ok {
		for _, i := range ds {
			dep := i.(string)

			if strings.Index(dep, "?") == 0 {
				strs := strings.Split(dep, " ")
				deps[strs[1]] = false
			} else {
				deps[strings.Split(dep, " ")[0]] = true
			}
		}
	}

	return &ModInfo{
		Name:         info["name"].(string),
		Path:         path,
		Dependencies: deps,
	}
}

func loadModData(L *lua.LState, mod *ModInfo) {
	pkg := L.GetGlobal("package").(*lua.LTable)
	startPkgPath := pkg.RawGetString("path")

	path := mod.Path

	// add this and all subdirs to package.path
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			pr := strings.Replace(p, "\\", "/", -1) // fml windows
			str := fmt.Sprintf("package.path = package.path .. \";%s/?.lua\"", pr)
			err = L.DoString(str)
			if err != nil {
				fmt.Println("err:", err)
			}
		}
		return nil
	})

	err := L.DoFile(path + "/data.lua")
	if err != nil {
		fmt.Println("data err:", err)
		return
	}

	// clean up package.path
	pkg.RawSetString("path", startPkgPath)
	fmt.Printf("loaded mod %v\n", mod)
}

func loadLocale(mods []*ModInfo) map[string]map[string]string {
	types := []string{"entity", "item", "fluid", "recipe"}

	locale := make(map[string]map[string]string)
	for _, t := range types {
		locale[t] = make(map[string]string)
	}

	for _, m := range mods {
		cfgs, err := ioutil.ReadDir(m.Path + "/locale/en")
		if err != nil {
			continue
		}

		for _, c := range cfgs {
			cfg, err := ini.Load(fmt.Sprintf("%s/locale/en/%s", m.Path, c.Name()))
			if err != nil {
				continue
			}

			for _, t := range types {
				cfg, err := cfg.GetSection(t + "-name")
				if err != nil {
					continue
				}

				for _, k := range cfg.KeyStrings() {
					v, err := cfg.GetKey(k)
					if err != nil {
						fmt.Println("err getting key???:", err)
						continue
					}
					locale[t][k] = v.MustString("")
				}
			}
		}
	}

	return locale
}

func getRecipes(L *lua.LState) []*Recipe {
	recipes :=
		L.GetGlobal("data").(*lua.LTable).
			RawGet(lua.LString("raw")).(*lua.LTable).
			RawGet(lua.LString("recipe")).(*lua.LTable)

	recs := make([]*Recipe, 0)

	recipes.ForEach(func(k, v lua.LValue) {
		recipe := v.(*lua.LTable)
		name := string(recipe.RawGetString("name").(lua.LString))

		var ing, res lua.LValue

		ing = recipe.RawGetString("ingredients")
		if ing.Type() == lua.LTNil {
			norm := recipe.RawGetString("normal").(*lua.LTable)
			ing = norm.RawGetString("ingredients")
			res = norm.RawGetString("results")
			if res.Type() == lua.LTNil {
				res = norm.RawGetString("result")
			}
		}

		if res == nil || res.Type() == lua.LTNil {
			res = recipe.RawGetString("results")
			if res.Type() == lua.LTNil {
				res = recipe.RawGetString("result")
			}
		}

		recs = append(recs, &Recipe{
			Name:        name,
			Ingredients: parseItems(ing),
			Results:     parseItems(res),
		})
	})

	return recs
}

func parseItems(v lua.LValue) map[string]float64 {
	items := make(map[string]float64)

	if v.Type() == lua.LTString {
		return map[string]float64{v.String(): 1}
	}

	t, ok := v.(*lua.LTable)
	if !ok {
		fmt.Println("invalid item!", v)
		return nil
	}

	t.ForEach(func(_, v lua.LValue) {
		item := v.(*lua.LTable)

		name, ok := item.RawGetString("name").(lua.LString)
		if !ok {
			name, ok = item.RawGetInt(1).(lua.LString)
		}

		amount, ok := item.RawGetString("amount").(lua.LNumber)
		if !ok {
			amount, ok = item.RawGetInt(2).(lua.LNumber)
		}

		items[string(name)] = float64(amount)
	})

	return items
}
