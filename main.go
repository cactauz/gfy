package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func main() {
	L := lua.NewState()
	setupState(L)

	loadMods(L)

	recipes := getRecipes(L)

	for _, r := range recipes {
		fmt.Println(r)
	}

	fmt.Println("found a total of", len(recipes), "recipes")
}

type Recipe struct {
	Name        string
	Ingredients map[string]float64
	Results     map[string]float64
}

func (r *Recipe) String() string {
	ings := ""
	for k, v := range r.Ingredients {
		ings += fmt.Sprintf("\t%s x %g\n", k, v)
	}

	res := ""
	for k, v := range r.Results {
		res += fmt.Sprintf("\t%s x %g\n", k, v)
	}

	return fmt.Sprintf("%s:\n%sresults:\n%s", r.Name, ings, res)
}

func setupState(L *lua.LState) {
	guiStyle := L.NewTable()
	guiStyle.RawSetString("default", L.NewTable())

	raw := L.NewTable()
	raw.RawSetString("gui-style", guiStyle)

	L.SetGlobal("mods", L.NewTable())

	// TODO: populate
	settings := L.NewTable()
	settings.RawSetString("startup", L.NewTable())
	L.SetGlobal("settings", settings)

	err := L.DoFile("./data.lua")
	if err != nil {
		fmt.Println("err:", err)
		return
	}

	rDiff := L.NewTable()
	rDiff.RawSetString("normal", lua.LString("normal"))
	rDiff.RawSetString("expensive", lua.LString("expensive"))

	diffS := L.NewTable()
	diffS.RawSetString("recipe_difficulty", rDiff)
	diffS.RawSetString("technology_difficulty", rDiff)

	dirs := L.NewTable()
	dirs.RawSetString("north", lua.LString("north"))
	dirs.RawSetString("south", lua.LString("south"))
	dirs.RawSetString("east", lua.LString("east"))
	dirs.RawSetString("west", lua.LString("west"))

	defines := L.NewTable()
	defines.RawSetString("difficulty_settings", diffS)
	defines.RawSetString("direction", dirs)
	L.SetGlobal("defines", defines)

	str := "package.path = package.path .. \";./factorio-data-master/core/lualib/?.lua\""
	L.DoString(str)
}

func loadModSettings(L *lua.LState, mods []*ModInfo) {
	settingState := lua.NewState()
	settingState.DoFile("./data.lua")

	ms := settingState.NewTable()
	for _, m := range mods {
		ms.RawSetString(m.Name, lua.LBool(true))
	}
	settingState.SetGlobal("mods", ms)

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
	Name         string
	Path         string
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
	path := mod.Path
	str := fmt.Sprintf("package.path = package.path .. \";%s/?.lua\"", path)
	err := L.DoString(str)
	if err != nil {
		fmt.Println(err)
	}

	prototypes, err := ioutil.ReadDir(path + "/prototypes")
	if err == nil {
		str = fmt.Sprintf("package.path = package.path .. \";%s/prototypes/?.lua\"", path)
		err = L.DoString(str)
		if err != nil {
			fmt.Println("err:", err)
		}

		for _, p := range prototypes {
			if p.IsDir() {
				str = fmt.Sprintf("package.path = package.path .. \";%s/prototypes/%s/?.lua\"", path, p.Name())
				err = L.DoString(str)
				if err != nil {
					fmt.Println("err:", err)
				}
			}
		}
	}

	err = L.DoFile(path + "/data.lua")
	if err != nil {
		fmt.Println("data err:", err)
		return
	}

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

func loadMods(L *lua.LState) {
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

	mods := make([]*ModInfo, 0)
	modNames := map[string]bool{
		"base": true,
		"core": true,
	}
	loadedMods := map[string]bool{
		"base": true,
		"core": true,
	}

	fis, err := ioutil.ReadDir("./mods")
	if err != nil {
		fmt.Println("err:", err)
		return
	}

	for _, fi := range fis {
		if fi.IsDir() {
			path := fmt.Sprintf("./mods/%s", fi.Name())
			_, err := ioutil.ReadFile(path + "/data.lua")

			if err == nil {
				info := loadModInfo(path)
				mods = append(mods, info)
				modNames[info.Name] = true
			}
		}
	}

	sort.Slice(mods, func(i, j int) bool {
		return mods[i].Name < mods[j].Name
	})

	loadModSettings(L, mods)

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
				fmt.Println("loading mod:", mod)
				loadModData(L, mod)
				loadedMods[mod.Name] = true
				mods = append(mods[:i], mods[i+1:]...)
			}
		}
	}
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
