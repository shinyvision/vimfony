package php

import (
	"strings"

	"github.com/shinyvision/vimfony/internal/config"
)

func (ctx *analysisContext) loadExternalClass(simple, fqcn string, classMethods map[string]*methodSet, extendsMap map[string][]string, fullNames map[string]string) {
	data := ctx.ensureExternalClassLoaded(fqcn)
	if data.methods != nil {
		classMethods[simple] = data.methods
	}
	if len(data.extends) > 0 {
		extendsMap[simple] = cloneStrings(data.extends)
	}
	if fullNames != nil {
		fullNames[simple] = normalizeFQN(fqcn)
	}
}

func (ctx *analysisContext) externalExtendsFor(fqcn string) []string {
	data := ctx.ensureExternalClassLoaded(fqcn)
	return cloneStrings(data.extends)
}

func (ctx *analysisContext) ensureExternalClassLoaded(fqcn string) externalClassData {
	fqcn = normalizeFQN(fqcn)
	if fqcn == "" || ctx.autoload.IsEmpty() {
		return externalClassData{}
	}
	if ctx.loaded == nil {
		ctx.loaded = make(map[string]externalClassData)
	}
	if data, ok := ctx.loaded[fqcn]; ok {
		return data
	}
	path, ok := config.AutoloadResolve(fqcn, ctx.autoload, ctx.root)
	if !ok {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}

	if ctx.store == nil {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}

	doc, err := ctx.store.Get(path)
	if err != nil {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}

	var extMethods map[string]*methodSet
	var extClasses map[uint32]ClassInfo

	index := doc.Index()
	extClasses = index.Classes
	extMethods = make(map[string]*methodSet)

	for _, fn := range index.PrivateFunctions {
		addClassMethod(extMethods, fn, "private")
	}
	for _, fn := range index.ProtectedFunctions {
		addClassMethod(extMethods, fn, "protected")
	}
	for _, fn := range index.PublicFunctions {
		addClassMethod(extMethods, fn, "public")
	}

	for _, info := range extClasses {
		full := normalizeFQN(info.FQN)
		if full == "" {
			continue
		}
		entry := externalClassData{
			methods: extMethods[info.Name],
			extends: cloneStrings(info.Extends),
		}
		ctx.loaded[full] = entry
	}

	if data, ok := ctx.loaded[fqcn]; ok {
		return data
	}
	ctx.loaded[fqcn] = externalClassData{}
	return ctx.loaded[fqcn]
}

func addClassMethod(methods map[string]*methodSet, fn FunctionInfo, visibility string) {
	// Name is "ClassName::MethodName"
	parts := strings.SplitN(fn.Name, "::", 2)
	if len(parts) != 2 {
		return
	}

	className := parts[0]
	fn.Name = parts[1] // Function passed as value

	if _, ok := methods[className]; !ok {
		methods[className] = &methodSet{}
	}
	set := methods[className]
	switch visibility {
	case "private":
		set.private = append(set.private, fn)
	case "protected":
		set.protected = append(set.protected, fn)
	case "public":
		set.public = append(set.public, fn)
	}
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
