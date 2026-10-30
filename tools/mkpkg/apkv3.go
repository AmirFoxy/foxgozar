package main

// An APKv3 package writer (OpenWrt 25.12+), so packages can be built on any
// OS without apk-tools. Follows apk-tools 3.0.5: src/adb.c, src/apk_adb.c,
// src/app_mkpkg.c, matching what `apk mkpkg` writes with its defaults
// (compat 3.0.0_pre1, deflate, unsigned).

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	adbTypeInt    = 0x10000000
	adbTypeInt32  = 0x20000000
	adbTypeInt64  = 0x30000000
	adbTypeBlob8  = 0x80000000
	adbTypeBlob16 = 0x90000000
	adbTypeBlob32 = 0xa0000000
	adbTypeArray  = 0xd0000000
	adbTypeObject = 0xe0000000

	adbBlockADB  = 0
	adbBlockData = 2

	schemaPackage = 0x676b6370 // "pckg"

	// Field indexes from apk_adb.h.
	piName, piVersion, piHashes, piDescription, piArch, piLicense = 1, 2, 3, 4, 5, 6
	piURL, piInstalledSize, piDepends, piMax                      = 9, 12, 15, 22
	aclMode, aclUser, aclGroup, aclMax                            = 1, 2, 3, 5
	fiName, fiACL, fiSize, fiMtime, fiHashes, fiMax               = 1, 2, 3, 4, 5, 7
	diName, diACL, diFiles, diMax                                 = 1, 2, 3, 4
	pkgInfo, pkgPaths, pkgScripts, pkgMax                         = 1, 2, 3, 6
	depName, depMax                                               = 1, 4
	scriptsMax                                                    = 8
)

// apk script types, as named on the mkpkg command line.
var scriptIndex = map[string]int{
	"trigger": 1, "pre-install": 2, "post-install": 3, "pre-deinstall": 4,
	"post-deinstall": 5, "pre-upgrade": 6, "post-upgrade": 7,
}

// adb is the metadata tree being written: values are 32-bit, a type in the
// top four bits and an offset (or a small integer) in the rest.
type adb struct{ buf []byte }

func newADB() *adb { return &adb{buf: make([]byte, 8)} } // header: versions, reserved, root

func (a *adb) raw(b []byte, align int) uint32 {
	for len(a.buf)%align != 0 {
		a.buf = append(a.buf, 0)
	}
	off := len(a.buf)
	a.buf = append(a.buf, b...)
	return uint32(off)
}

func (a *adb) blob(b []byte) uint32 {
	n := len(b)
	switch {
	case n == 0:
		return 0
	case n > 0xffff:
		return adbTypeBlob32 | a.raw(append(binary.LittleEndian.AppendUint32(nil, uint32(n)), b...), 4)
	case n > 0xff:
		return adbTypeBlob16 | a.raw(append(binary.LittleEndian.AppendUint16(nil, uint16(n)), b...), 2)
	}
	return adbTypeBlob8 | a.raw(append([]byte{byte(n)}, b...), 1)
}

func (a *adb) str(s string) uint32 { return a.blob([]byte(s)) }

func (a *adb) int(v uint64) uint32 {
	switch {
	case v >= 1<<32:
		return adbTypeInt64 | a.raw(binary.LittleEndian.AppendUint64(nil, v), 4)
	case v >= 0x10000000:
		return adbTypeInt32 | a.raw(binary.LittleEndian.AppendUint32(nil, uint32(v)), 4)
	}
	return adbTypeInt | uint32(v)
}

// obj writes fields[1:] (fields[0] is the count slot); trailing nulls are
// dropped and an empty object is null, as adb_w_obj does.
func (a *adb) obj(fields []uint32, typ uint32) uint32 {
	n := len(fields)
	for n > 1 && fields[n-1] == 0 {
		n--
	}
	if n <= 1 {
		return 0
	}
	out := make([]byte, 0, 4*n)
	out = binary.LittleEndian.AppendUint32(out, uint32(n))
	for _, v := range fields[1:n] {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	return typ | a.raw(out, 4)
}

func (a *adb) array(items []uint32) uint32 {
	return a.obj(append([]uint32{0}, items...), adbTypeArray)
}

// apk_blob_sort: bytewise, then shorter first.
func blobLess(x, y string) bool { return x < y }

func block(typ uint32, payload []byte) []byte {
	raw := 4 + len(payload)
	out := binary.LittleEndian.AppendUint32(nil, typ<<30|uint32(raw))
	out = append(out, payload...)
	for len(out)%8 != 0 {
		out = append(out, 0)
	}
	return out
}

type apkMeta struct {
	Name, Version, Arch, Description, License, URL string
	Depends                                        []string
	Scripts                                        map[string]string
}

// buildAPK packs files (paths like "./usr/bin/x") into an APKv3 package.
func buildAPK(m apkMeta, files []file, mtime int64) ([]byte, error) {
	// Every directory on the way to a file is listed, the root included
	// (compat 3.0.0_pre1), each with its files sorted by name.
	type dirent struct {
		name  string
		files []file
	}
	dirs := map[string]*dirent{"": {}}
	var installed uint64
	for _, f := range files {
		p := strings.TrimPrefix(f.path, "./")
		d, base := path.Split(p)
		d = strings.TrimSuffix(d, "/")
		for x := d; x != "" && x != "."; x = path.Dir(x) {
			if dirs[x] == nil {
				dirs[x] = &dirent{name: x}
			}
		}
		dirs[d].files = append(dirs[d].files, file{base, f.mode, f.data})
		installed += uint64(len(f.data))
	}
	names := make([]string, 0, len(dirs))
	for n := range dirs {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return blobLess(names[i], names[j]) })

	a := newADB()
	acl := func(mode int64) uint32 {
		f := make([]uint32, aclMax)
		f[aclMode] = a.int(uint64(mode))
		f[aclUser] = a.str("root")
		f[aclGroup] = a.str("root")
		return a.obj(f, adbTypeObject)
	}

	var pathVals []uint32
	for _, dn := range names {
		d := dirs[dn]
		sort.Slice(d.files, func(i, j int) bool { return blobLess(d.files[i].path, d.files[j].path) })
		var fileVals []uint32
		for _, f := range d.files {
			sum := sha256.Sum256(f.data)
			fv := make([]uint32, fiMax)
			fv[fiName] = a.str(f.path)
			fv[fiHashes] = a.blob(sum[:])
			fv[fiMtime] = a.int(uint64(mtime))
			fv[fiSize] = a.int(uint64(len(f.data)))
			fv[fiACL] = acl(f.mode)
			fileVals = append(fileVals, a.obj(fv, adbTypeObject))
		}
		dv := make([]uint32, diMax)
		dv[diName] = a.str(dn)
		dv[diACL] = acl(0o755)
		dv[diFiles] = a.array(fileVals)
		pathVals = append(pathVals, a.obj(dv, adbTypeObject))
	}

	pi := make([]uint32, piMax)
	pi[piName] = a.str(m.Name)
	pi[piVersion] = a.str(m.Version)
	pi[piDescription] = a.str(m.Description)
	pi[piArch] = a.str(m.Arch)
	pi[piLicense] = a.str(m.License)
	pi[piURL] = a.str(m.URL)
	if installed == 0 {
		installed = 1
	}
	pi[piInstalledSize] = a.int(installed)
	deps := append([]string(nil), m.Depends...)
	sort.Slice(deps, func(i, j int) bool { return blobLess(deps[i], deps[j]) })
	var depVals []uint32
	for i, d := range deps {
		if i > 0 && deps[i-1] == d {
			continue
		}
		if strings.ContainsAny(d, "<>=!~") {
			return nil, fmt.Errorf("dependency %q: only plain names are supported", d)
		}
		dv := make([]uint32, depMax)
		dv[depName] = a.str(d)
		depVals = append(depVals, a.obj(dv, adbTypeObject))
	}
	pi[piDepends] = a.array(depVals)
	// The unique id: SHA-1 sized, filled in below with the start of the
	// SHA-256 of the finished tree (written raw, never shared).
	uid := a.raw([]byte{20}, 1)
	a.raw(make([]byte, 20), 1)
	pi[piHashes] = adbTypeBlob8 | uid

	pkg := make([]uint32, pkgMax)
	pkg[pkgInfo] = a.obj(pi, adbTypeObject)
	pkg[pkgPaths] = a.array(pathVals)
	if len(m.Scripts) > 0 {
		sv := make([]uint32, scriptsMax)
		for typ, body := range m.Scripts {
			i, ok := scriptIndex[typ]
			if !ok {
				return nil, fmt.Errorf("unknown script type %q", typ)
			}
			sv[i] = a.str(body)
		}
		pkg[pkgScripts] = a.obj(sv, adbTypeObject)
	}
	root := a.obj(pkg, adbTypeObject)
	binary.LittleEndian.PutUint32(a.buf[4:], root)

	sum := sha256.Sum256(a.buf)
	copy(a.buf[uid+1:uid+21], sum[:20])

	var plain bytes.Buffer
	plain.WriteString("ADB.")
	plain.Write(binary.LittleEndian.AppendUint32(nil, schemaPackage))
	plain.Write(block(adbBlockADB, a.buf))
	// File contents follow in the same order as the tree: directory i,
	// file j, both counted from 1.
	for i, dn := range names {
		for j, f := range dirs[dn].files {
			if len(f.data) == 0 {
				continue
			}
			hdr := binary.LittleEndian.AppendUint32(nil, uint32(i+1))
			hdr = binary.LittleEndian.AppendUint32(hdr, uint32(j+1))
			plain.Write(block(adbBlockData, append(hdr, f.data...)))
		}
	}

	var out bytes.Buffer
	out.WriteString("ADBd")
	zw, _ := flate.NewWriter(&out, flate.BestCompression)
	if _, err := zw.Write(plain.Bytes()); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
