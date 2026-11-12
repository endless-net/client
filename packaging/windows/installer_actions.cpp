#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <iphlpapi.h>
#include <netioapi.h>
#include <msiquery.h>
#include <shlobj.h>
#include <algorithm>
#include <cwctype>
#include <string>
#include <set>
#include <vector>

namespace {
// Adapted from the public Client UI installer preflight. This producer owns
// its standalone package; no Client UI binary/source is needed at build time.
UINT Fail(MSIHANDLE session, const wchar_t* stage, DWORD error = 0) {
  auto record = MsiCreateRecord(2);
  MsiRecordSetStringW(record, 0, L"EndlessNet preflight failed: [1], Windows status [2]");
  MsiRecordSetStringW(record, 1, stage);
  MsiRecordSetInteger(record, 2, static_cast<int>(error));
  MsiProcessMessage(session, INSTALLMESSAGE_INFO, record);
  MsiCloseHandle(record);
  return ERROR_INSTALL_FAILURE;
}
bool LocalEndpoint(MSIHANDLE session) {
  wchar_t port[6]{}; DWORD length = 6;
  if (MsiGetPropertyW(session, L"EndlessNetListenPort", port, &length) != ERROR_SUCCESS || !length) return false;
  unsigned number = 0;
  for (DWORD i = 0; i < length; ++i) {
    if (port[i] < L'0' || port[i] > L'9') return false;
    number = number * 10 + port[i] - L'0';
  }
  if (!number || number > 65535) return false;
  MIB_IPFORWARD_TABLE2* routes = nullptr;
  if (GetIpForwardTable2(AF_INET, &routes) != NO_ERROR) return false;
  std::set<NET_IFINDEX> interfaces;
  if (routes->NumEntries <= 1024) {
    for (ULONG i = 0; i < routes->NumEntries; ++i) {
      const auto& row = routes->Table[i];
      if (row.DestinationPrefix.PrefixLength == 0 && !row.Loopback) interfaces.insert(row.InterfaceIndex);
    }
  }
  FreeMibTable(routes);
  // Same no-NAT, single-default-interface model as the existing Client runtime.
  // This is an OS address, never a fabricated public/NAT endpoint.
  if (interfaces.size() != 1) return false;
  MIB_UNICASTIPADDRESS_TABLE* addresses = nullptr;
  if (GetUnicastIpAddressTable(AF_INET, &addresses) != NO_ERROR) return false;
  std::set<std::wstring> candidates;
  if (addresses->NumEntries <= 1024) {
    for (ULONG i = 0; i < addresses->NumEntries; ++i) {
      const auto& row = addresses->Table[i];
      if (row.InterfaceIndex != *interfaces.begin() || row.SkipAsSource || row.DadState != IpDadStatePreferred) continue;
      const auto& addr = row.Address.Ipv4.sin_addr;
      const auto* bytes = reinterpret_cast<const unsigned char*>(&addr);
      if (!bytes[0] || bytes[0] == 127 || bytes[0] >= 224 || (bytes[0] == 169 && bytes[1] == 254)) continue;
      wchar_t text[INET_ADDRSTRLEN]{};
      if (InetNtopW(AF_INET, const_cast<IN_ADDR*>(&addr), text, INET_ADDRSTRLEN)) candidates.insert(text);
    }
  }
  FreeMibTable(addresses);
  if (candidates.size() != 1) return false;
  const auto endpoint = *candidates.begin() + L":" + port;
  return MsiSetPropertyW(session, L"ENDLESSNET_LOCAL_ENDPOINT", endpoint.c_str()) == ERROR_SUCCESS;
}

std::wstring CanonicalGuid(const GUID& guid) {
  wchar_t text[39]{};
  if (!StringFromGUID2(guid, text, 39)) return {};
  std::wstring result(text + 1, 36);
  std::transform(result.begin(), result.end(), result.begin(), towlower);
  return result;
}
bool TargetIs(MSIHANDLE session, const wchar_t* directory, REFKNOWNFOLDERID folder,
              const wchar_t* suffix) {
  wchar_t target[32768]{}; DWORD length = 32768;
  PWSTR known = nullptr;
  if (MsiGetTargetPathW(session, directory, target, &length) != ERROR_SUCCESS ||
      FAILED(SHGetKnownFolderPath(folder, KF_FLAG_DONT_VERIFY, nullptr, &known))) return false;
  const auto expected = std::wstring(known) + suffix;
  CoTaskMemFree(known);
  return _wcsicmp(target, expected.c_str()) == 0;
}
bool SafeExistingPath(MSIHANDLE session, const wchar_t* directory) {
  wchar_t target[32768]{}; DWORD length = 32768;
  if (MsiGetTargetPathW(session, directory, target, &length) != ERROR_SUCCESS) return false;
  std::wstring path(target);
  while (path.size() > 3) {
    if (path.back() == L'\\') path.pop_back();
    const auto attrs = GetFileAttributesW(path.c_str());
    if (attrs == INVALID_FILE_ATTRIBUTES) {
      const auto error = GetLastError();
      if (error != ERROR_FILE_NOT_FOUND && error != ERROR_PATH_NOT_FOUND) return false;
    } else if (!(attrs & FILE_ATTRIBUTE_DIRECTORY) || (attrs & FILE_ATTRIBUTE_REPARSE_POINT)) return false;
    const auto slash = path.find_last_of(L'\\');
    if (slash == std::wstring::npos) return false;
    path.resize(slash + 1);
  }
  return true;
}
bool PropertyPresent(MSIHANDLE session, const wchar_t* name) {
  wchar_t buffer[2]{}; DWORD length = 2;
  const auto result = MsiGetPropertyW(session, name, buffer, &length);
  return (result == ERROR_SUCCESS || result == ERROR_MORE_DATA) && length > 0;
}
bool ServiceAvailable(MSIHANDLE session) {
  wchar_t product[39]{};
  const auto combined = MsiEnumRelatedProductsW(L"{FA0F488C-9869-47EC-A2B0-F41313F40657}", 0, 0, product);
  // Do not trust a command-line override of FindRelatedProducts properties.
  if (combined != ERROR_NO_MORE_ITEMS) return false;
  const auto manager = OpenSCManagerW(nullptr, nullptr, SC_MANAGER_CONNECT);
  if (!manager) return false;
  const auto service = OpenServiceW(manager, L"endlessnet-client", SERVICE_QUERY_CONFIG);
  const auto error = GetLastError();
  CloseServiceHandle(manager);
  if (!service) return error == ERROR_SERVICE_DOES_NOT_EXIST;
  // A fresh standalone install must not take over an unmanaged/other-product
  // service. Standard removal of the old product preserves permanent state.
  const auto own = MsiEnumRelatedProductsW(L"{9B78E1DF-849D-588C-B5D6-DC393A1B033B}", 0, 0, product);
  DWORD size = 0;
  QueryServiceConfigW(service, nullptr, 0, &size);
  if (own != ERROR_SUCCESS || size < sizeof(QUERY_SERVICE_CONFIGW) || size > 65536) {
    CloseServiceHandle(service); return false;
  }
  std::vector<unsigned char> storage(size);
  auto* config = reinterpret_cast<QUERY_SERVICE_CONFIGW*>(storage.data());
  wchar_t target[32768]{}; DWORD length = 32768;
  bool valid = QueryServiceConfigW(service, config, size, &size) &&
      MsiGetTargetPathW(session, L"AppRoot", target, &length) == ERROR_SUCCESS;
  if (valid) {
    const auto expected = L"\"" + std::wstring(target) + L"client.exe\" installed-run";
    valid = config->dwServiceType == SERVICE_WIN32_OWN_PROCESS && config->lpServiceStartName &&
        _wcsicmp(config->lpServiceStartName, L"LocalSystem") == 0 && config->lpBinaryPathName &&
        _wcsicmp(config->lpBinaryPathName, expected.c_str()) == 0;
  }
  CloseServiceHandle(service);
  return valid;
}
bool PreserveServiceSettings(MSIHANDLE session) {
  constexpr auto serviceKey = L"SYSTEM\\CurrentControlSet\\Services\\endlessnet-client";
  DWORD size = 0;
  auto status = RegGetValueW(HKEY_LOCAL_MACHINE, serviceKey, L"Environment",
      RRF_RT_REG_MULTI_SZ | RRF_SUBKEY_WOW6464KEY, nullptr, nullptr, &size);
  if (status == ERROR_FILE_NOT_FOUND || status == ERROR_PATH_NOT_FOUND) return true;
  if (status != ERROR_SUCCESS || size < 2 * sizeof(wchar_t) || size > 65536 || size % sizeof(wchar_t)) return false;
  std::vector<wchar_t> data(size / sizeof(wchar_t));
  status = RegGetValueW(HKEY_LOCAL_MACHINE, serviceKey, L"Environment",
      RRF_RT_REG_MULTI_SZ | RRF_SUBKEY_WOW6464KEY, nullptr, data.data(), &size);
  if (status != ERROR_SUCCESS || data.back() || data[data.size() - 2]) return false;
  std::set<std::wstring> seen;
  for (const auto* p = data.data(); *p; p += wcslen(p) + 1) {
    const std::wstring entry(p);
    const auto equals = entry.find(L'=');
    if (equals == std::wstring::npos) return false;
    const auto name = entry.substr(0, equals);
    if (name.rfind(L"ENDLESSNET_CLIENT_", 0) != 0 || !seen.insert(name).second ||
        name.find_first_not_of(L"ABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789") != std::wstring::npos ||
        entry.find_first_of(L"\r\n") != std::wstring::npos) return false;
    const auto property = L"ENSET_" + name;
    // Endpoint is always observed from this machine's current interface.
    if (name == L"ENDLESSNET_CLIENT_CONNECTION_ENDPOINT") continue;
    // The producer owns this lifecycle bound; retain the validated package
    // value so upgrades receive stop-budget fixes rather than an old default.
    if (name == L"ENDLESSNET_CLIENT_STOP_TIMEOUT") continue;
    // Preserve only fields in this package, never introduce arbitrary registry
    // properties. New producer settings retain their validated package value.
    if (PropertyPresent(session, property.c_str()) &&
        MsiSetPropertyW(session, property.c_str(), entry.substr(equals + 1).c_str()) != ERROR_SUCCESS) return false;
  }
  return true;
}
bool ResolvePackagedSettings(MSIHANDLE session) {
  const auto database = MsiGetActiveDatabase(session);
  MSIHANDLE view = 0;
  if (!database || MsiDatabaseOpenViewW(database, L"SELECT `Property`, `Value` FROM `Property`", &view) != ERROR_SUCCESS) {
    if (database) MsiCloseHandle(database);
    return false;
  }
  bool ok = MsiViewExecute(view, 0) == ERROR_SUCCESS;
  MSIHANDLE row = 0;
  UINT status = ERROR_SUCCESS;
  unsigned count = 0;
  while (ok && (status = MsiViewFetch(view, &row)) == ERROR_SUCCESS) {
    wchar_t name[256]{}; DWORD length = 256;
    ok = ++count <= 1024 && MsiRecordGetStringW(row, 1, name, &length) == ERROR_SUCCESS;
    if (ok && wcsncmp(name, L"ENSET_ENDLESSNET_CLIENT_", wcslen(L"ENSET_ENDLESSNET_CLIENT_")) == 0) {
      std::vector<wchar_t> value(32768); length = static_cast<DWORD>(value.size());
      ok = MsiRecordGetStringW(row, 2, value.data(), &length) == ERROR_SUCCESS;
      const auto format = MsiCreateRecord(0);
      std::vector<wchar_t> resolved(32768); DWORD resolvedLength = static_cast<DWORD>(resolved.size());
      ok = ok && format && MsiRecordSetStringW(format, 0, value.data()) == ERROR_SUCCESS &&
          MsiFormatRecordW(session, format, resolved.data(), &resolvedLength) == ERROR_SUCCESS &&
          MsiSetPropertyW(session, name, resolved.data()) == ERROR_SUCCESS;
      if (format) MsiCloseHandle(format);
    }
    MsiCloseHandle(row); row = 0;
  }
  if (row) MsiCloseHandle(row);
  MsiCloseHandle(view); MsiCloseHandle(database);
  return ok && status == ERROR_NO_MORE_ITEMS;
}
}

// Immediate, read-only preflight. Only MSI properties are changed; Windows
// Installer owns the later registry, directory, service and file transactions.
extern "C" __declspec(dllexport) UINT __stdcall PrepareMachine(MSIHANDLE session) {
  SYSTEM_INFO system{}; GetNativeSystemInfo(&system);
  if (system.wProcessorArchitecture != PROCESSOR_ARCHITECTURE_AMD64) return Fail(session, L"architecture", system.wProcessorArchitecture);
  // Windows Installer applies a version lie even to RtlGetVersion in its CA
  // server. Read the protected machine installation facts instead.
  constexpr auto osKey = L"SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion";
  wchar_t buildText[16]{}, installation[32]{};
  DWORD major = 0, size = sizeof(major);
  auto rc = RegGetValueW(HKEY_LOCAL_MACHINE, osKey, L"CurrentMajorVersionNumber", RRF_RT_REG_DWORD | RRF_SUBKEY_WOW6464KEY, nullptr, &major, &size);
  if (rc != ERROR_SUCCESS || major < 10) return Fail(session, L"OS major", rc);
  size = sizeof(buildText);
  rc = RegGetValueW(HKEY_LOCAL_MACHINE, osKey, L"CurrentBuildNumber", RRF_RT_REG_SZ | RRF_SUBKEY_WOW6464KEY, nullptr, buildText, &size);
  if (rc != ERROR_SUCCESS) return Fail(session, L"OS build registry", rc);
  DWORD build = 0;
  for (const auto* p = buildText; *p; ++p) {
    if (*p < L'0' || *p > L'9' || build > (MAXDWORD - (*p - L'0')) / 10) return Fail(session, L"OS build format");
    build = build * 10 + *p - L'0';
  }
  if (build < 22000) return Fail(session, L"OS build", build);
  size = sizeof(installation);
  rc = RegGetValueW(HKEY_LOCAL_MACHINE, osKey, L"InstallationType", RRF_RT_REG_SZ | RRF_SUBKEY_WOW6464KEY, nullptr, installation, &size);
  if (rc != ERROR_SUCCESS || wcscmp(installation, L"Client") != 0) return Fail(session, L"OS installation type", rc);
  if (!TargetIs(session, L"AppRoot", FOLDERID_ProgramFilesX64, L"\\EndlessNet\\") ||
      !TargetIs(session, L"ClientData", FOLDERID_ProgramData, L"\\EndlessNet\\Client\\") ||
      !TargetIs(session, L"ClientState", FOLDERID_ProgramData, L"\\EndlessNet\\Client\\state\\"))
    return Fail(session, L"installation directories");
  if (!SafeExistingPath(session, L"AppRoot") || !SafeExistingPath(session, L"ClientState"))
    return Fail(session, L"installation path reparse or access");
  if (!ServiceAvailable(session)) return Fail(session, L"existing service belongs to another installation");
  if (!LocalEndpoint(session)) return Fail(session, L"local IPv4 endpoint", GetLastError());

  wchar_t previous[37]{}; DWORD length = sizeof(previous);
  const auto status = RegGetValueW(HKEY_LOCAL_MACHINE, L"SOFTWARE\\EndlessNet\\Client",
      L"InstallationId", RRF_RT_REG_SZ | RRF_SUBKEY_WOW6464KEY, nullptr, previous, &length);
  GUID id{}; bool existing = false;
  if (status == ERROR_SUCCESS) {
    if (length != sizeof(previous) || previous[36] != 0) return Fail(session, L"existing identity length", length);
    const auto wrapped = L"{" + std::wstring(previous) + L"}";
    if (FAILED(IIDFromString(wrapped.c_str(), &id)) || IsEqualGUID(id, GUID_NULL) ||
        CanonicalGuid(id) != previous) return Fail(session, L"existing identity format");
    existing = true;
  } else if (status == ERROR_FILE_NOT_FOUND || status == ERROR_PATH_NOT_FOUND) {
    if (FAILED(CoCreateGuid(&id))) return Fail(session, L"create identity");
  } else return Fail(session, L"identity registry", status);
  if (MsiSetPropertyW(session, L"ENDLESSNET_INSTALLATION_ID", CanonicalGuid(id).c_str()) != ERROR_SUCCESS ||
      MsiSetPropertyW(session, L"ENDLESSNET_EXISTING_INSTALLATION", existing ? L"1" : L"") != ERROR_SUCCESS)
    return Fail(session, L"identity properties");
  // Read defaults from the MSI database, not caller-overridable properties;
  // expand machine facts once, then prefer actual pre-upgrade service values.
  if (!ResolvePackagedSettings(session) || !PreserveServiceSettings(session))
    return Fail(session, L"service configuration properties");
  return ERROR_SUCCESS;
}
