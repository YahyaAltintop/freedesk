//go:build windows

package firewall

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows APIs the check uses, all from System32: COM itself (ole32,
// oleaut32) and, through it, the Windows Firewall API (FirewallAPI.dll's
// HNetCfg.FwPolicy2 and HNetCfg.FwProducts) and the Network List Manager.
// Nothing is loaded from anywhere else, and nothing is changed: every call
// below reads.
var (
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")

	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCoCreateInstance    = ole32.NewProc("CoCreateInstance")
	procSysFreeString       = oleaut32.NewProc("SysFreeString")
	procVariantClear        = oleaut32.NewProc("VariantClear")
	procSafeArrayGetLBound  = oleaut32.NewProc("SafeArrayGetLBound")
	procSafeArrayGetUBound  = oleaut32.NewProc("SafeArrayGetUBound")
	procSafeArrayGetElement = oleaut32.NewProc("SafeArrayGetElement")
)

// Class and interface ids, from the MinGW-w64 netfw.h, netlistmgr.h and
// oaidl.h declarations.
var (
	clsidNetFwPolicy2       = windows.GUID{Data1: 0xe2b3c97f, Data2: 0x6ae1, Data3: 0x41ac, Data4: [8]byte{0x81, 0x7a, 0xf6, 0xf9, 0x21, 0x66, 0xd7, 0xdd}}
	iidINetFwPolicy2        = windows.GUID{Data1: 0x98325047, Data2: 0xc671, Data3: 0x4174, Data4: [8]byte{0x8d, 0x81, 0xde, 0xfc, 0xd3, 0xf0, 0x31, 0x86}}
	iidINetFwRule           = windows.GUID{Data1: 0xaf230d27, Data2: 0xbaba, Data3: 0x4e42, Data4: [8]byte{0xac, 0xed, 0xf5, 0x24, 0xf2, 0x2c, 0xfc, 0xe2}}
	clsidNetFwProducts      = windows.GUID{Data1: 0xcc19079b, Data2: 0x8272, Data3: 0x4d73, Data4: [8]byte{0xbb, 0x70, 0xcd, 0xb5, 0x33, 0x52, 0x7b, 0x61}}
	iidINetFwProducts       = windows.GUID{Data1: 0x39eb36e0, Data2: 0x2097, Data3: 0x40bd, Data4: [8]byte{0x8a, 0xf2, 0x63, 0xa1, 0x3b, 0x52, 0x53, 0x62}}
	clsidNetworkListManager = windows.GUID{Data1: 0xdcb00c01, Data2: 0x570f, Data3: 0x4a9b, Data4: [8]byte{0x8d, 0x69, 0x19, 0x9f, 0xdb, 0xa5, 0x72, 0x3b}}
	iidINetworkListManager  = windows.GUID{Data1: 0xdcb00000, Data2: 0x570f, Data3: 0x4a9b, Data4: [8]byte{0x8d, 0x69, 0x19, 0x9f, 0xdb, 0xa5, 0x72, 0x3b}}
	iidIEnumVARIANT         = windows.GUID{Data1: 0x00020404, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// vtable slots, from the same headers. IUnknown takes slots 0-2 and
// IDispatch 3-6, so a dual interface's own methods start at 7.
const (
	policy2FirewallEnabled = 8  // INetFwPolicy2::get_FirewallEnabled
	policy2Rules           = 18 // INetFwPolicy2::get_Rules
	rulesNewEnum           = 11 // INetFwRules::get__NewEnum
	ruleApplicationName    = 11 // INetFwRule::get_ApplicationName
	ruleDirection          = 27 // INetFwRule::get_Direction
	ruleEnabled            = 33 // INetFwRule::get_Enabled
	ruleProfiles           = 37 // INetFwRule::get_Profiles
	ruleAction             = 41 // INetFwRule::get_Action
	productsCount          = 7  // INetFwProducts::get_Count
	productsItem           = 9  // INetFwProducts::Item
	productRuleCategories  = 7  // INetFwProduct::get_RuleCategories
	productDisplayName     = 9  // INetFwProduct::get_DisplayName
	nlmGetNetworks         = 7  // INetworkListManager::GetNetworks
	enumNetworksNext       = 8  // IEnumNetworks::Next
	networkGetCategory     = 18 // INetwork::GetCategory
	enumVariantNext        = 3  // IEnumVARIANT::Next
)

const (
	sOK                 = 0
	sFalse              = 1
	rpcEChangedMode     = 0x80010106
	coinitMultithreaded = 0x0
	clsctxAll           = 0x17 // CLSCTX_INPROC_SERVER | INPROC_HANDLER | LOCAL_SERVER | REMOTE_SERVER

	vtI2       = 2
	vtI4       = 3
	vtDispatch = 9
	vtVariant  = 12
	vtUnknown  = 13
	vtUI1      = 17
	vtUI2      = 18
	vtUI4      = 19
	vtInt      = 22
	vtUInt     = 23
	vtArray    = 0x2000

	// maxArrayLen bounds how much of an array a property hands back is read.
	maxArrayLen = 64
)

// hresult is a failed COM call.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("HRESULT 0x%08X", uint32(h)) }

func failed(r uintptr) bool { return int32(uint32(r)) < 0 }

func hresultErr(what string, r uintptr) error {
	return fmt.Errorf("%s: %w", what, hresult(uint32(r)))
}

// method returns the address of the i-th entry of a COM object's vtable.
func method(obj unsafe.Pointer, i int) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtbl, uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// release calls IUnknown::Release, ignoring nil.
func release(obj unsafe.Pointer) {
	if obj != nil {
		_, _, _ = syscall.SyscallN(method(obj, 2), uintptr(obj))
	}
}

// queryInterface calls IUnknown::QueryInterface.
func queryInterface(obj unsafe.Pointer, iid *windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(obj, 0), uintptr(obj), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if failed(r) {
		return nil, hresultErr("QueryInterface", r)
	}
	if out == nil {
		return nil, errors.New("QueryInterface returned no interface")
	}
	return out, nil
}

// create makes an object of class clsid and returns its interface iid.
func create(clsid, iid *windows.GUID) (unsafe.Pointer, error) {
	var obj unsafe.Pointer
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxAll,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))
	if failed(r) {
		return nil, hresultErr("CoCreateInstance", r)
	}
	if obj == nil {
		return nil, errors.New("CoCreateInstance returned no object")
	}
	return obj, nil
}

// getInt32 reads a property whose value is a LONG or an enum.
func getInt32(obj unsafe.Pointer, slot int) (int32, error) {
	var v int32
	r, _, _ := syscall.SyscallN(method(obj, slot), uintptr(obj), uintptr(unsafe.Pointer(&v)))
	if failed(r) {
		return 0, hresult(uint32(r))
	}
	return v, nil
}

// getBool reads a VARIANT_BOOL property: 0 is false, VARIANT_TRUE (-1) true.
func getBool(obj unsafe.Pointer, slot int) (bool, error) {
	var v int16
	r, _, _ := syscall.SyscallN(method(obj, slot), uintptr(obj), uintptr(unsafe.Pointer(&v)))
	if failed(r) {
		return false, hresult(uint32(r))
	}
	return v != 0, nil
}

// getString reads a BSTR property and frees the BSTR. A null BSTR is the
// empty string.
func getString(obj unsafe.Pointer, slot int) (string, error) {
	var b *uint16
	r, _, _ := syscall.SyscallN(method(obj, slot), uintptr(obj), uintptr(unsafe.Pointer(&b)))
	if failed(r) {
		return "", hresult(uint32(r))
	}
	if b == nil {
		return "", nil
	}
	defer func() { _, _, _ = procSysFreeString.Call(uintptr(unsafe.Pointer(b))) }()
	return windows.UTF16PtrToString(b), nil
}

// variant mirrors VARIANT: a type tag, three reserved words, and a union the
// size of two pointers (24 bytes in all on 64-bit Windows, 16 on 32-bit).
// The union's first word holds an interface pointer (VT_DISPATCH), a
// SAFEARRAY pointer (VT_ARRAY) or, in its first bytes, a number (VT_I4…).
type variant struct {
	vt       uint16
	reserved [3]uint16
	val      unsafe.Pointer
	val2     unsafe.Pointer
}

// clear frees what v holds (VariantClear): it releases an interface and
// destroys an array.
func (v *variant) clear() {
	_, _, _ = procVariantClear.Call(uintptr(unsafe.Pointer(v)))
}

// int reads v as a whole number, if it holds one.
func (v *variant) int() (int64, bool) {
	p := unsafe.Pointer(&v.val)
	switch v.vt {
	case vtI2:
		return int64(*(*int16)(p)), true
	case vtI4, vtInt:
		return int64(*(*int32)(p)), true
	case vtUI1:
		return int64(*(*uint8)(p)), true
	case vtUI2:
		return int64(*(*uint16)(p)), true
	case vtUI4, vtUInt:
		return int64(*(*uint32)(p)), true
	}
	return 0, false
}

// ints reads v as whole numbers: one, or a one-dimensional array of them
// (of VARIANTs holding numbers, as INetFwProduct::RuleCategories hands
// them over, or of LONGs). Anything else reads as none.
func (v *variant) ints() []int64 {
	if v.vt&vtArray == 0 {
		if n, ok := v.int(); ok {
			return []int64{n}
		}
		return nil
	}
	array := v.val
	if array == nil {
		return nil
	}
	var lo, hi int32
	r1, _, _ := procSafeArrayGetLBound.Call(uintptr(array), 1, uintptr(unsafe.Pointer(&lo)))
	r2, _, _ := procSafeArrayGetUBound.Call(uintptr(array), 1, uintptr(unsafe.Pointer(&hi)))
	if failed(r1) || failed(r2) {
		return nil
	}
	var out []int64
	for i := lo; i <= hi && i-lo < maxArrayLen; i++ {
		switch v.vt &^ vtArray {
		case vtVariant:
			var e variant
			r, _, _ := procSafeArrayGetElement.Call(uintptr(array), uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&e)))
			if failed(r) {
				continue
			}
			if n, ok := e.int(); ok {
				out = append(out, n)
			}
			e.clear()
		case vtI4, vtInt:
			var n int32
			r, _, _ := procSafeArrayGetElement.Call(uintptr(array), uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&n)))
			if !failed(r) {
				out = append(out, int64(n))
			}
		}
	}
	return out
}
