"""Standalone Windows Client service/CLI MSI; no Flutter or Client UI input."""
import argparse
import ipaddress
import json
from pathlib import Path
import re
from urllib.parse import urlsplit
import uuid
import xml.etree.ElementTree as ET

NS = "http://wixtoolset.org/schemas/v4/wxs"
UPGRADE_CODE = "{9B78E1DF-849D-588C-B5D6-DC393A1B033B}"
COMBINED_UPGRADE_CODE = "{FA0F488C-9869-47EC-A2B0-F41313F40657}"
PUBLIC = "O:SYG:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)"
CONTAINER = "O:SYG:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;FRFX;;;BU)"
PRIVATE = "O:SYG:SYD:P(A;OICI;FA;;;SY)"
SERVICE = r"SYSTEM\CurrentControlSet\Services\endlessnet-client"
PAYLOAD = ("client.exe", "endlessnet.exe", "wintun.dll", "WINTUN-LICENSE.txt")


def configuration(defaults, server, endpoint, sid, state_directory):
    """Prepare temporary validation input; target facts are resolved again by MSI."""
    url = urlsplit(server)
    if (url.scheme != "https" or not url.hostname or url.username or url.password
            or url.path not in ("", "/") or url.query or url.fragment
            or url.port not in (None, 443) or not re.fullmatch(r"[a-z0-9.-]+", url.hostname)):
        raise ValueError("Server must be an HTTPS origin")
    if (not defaults or any(not re.fullmatch(r"ENDLESSNET_CLIENT_[A-Z0-9_]+", k)
                           or not isinstance(v, str) or any(c in v for c in "\0\r\n")
                           for k, v in defaults.items())):
        raise ValueError("Invalid producer defaults")
    if any(k in defaults for k in ("ENDLESSNET_CLIENT_INSTALLATION_ID", "ENDLESSNET_CLIENT_ALLOWED_SIDS",
                                  "ENDLESSNET_CLIENT_SERVICE_SID", "ENDLESSNET_CLIENT_STATE_DIRECTORY",
                                  "ENDLESSNET_CLIENT_CONNECTION_ENDPOINT", "ENDLESSNET_CLIENT_CONTROL_ORIGIN")):
        raise ValueError("Machine/server facts must not be producer defaults")
    address, port = endpoint.rsplit(":", 1)
    ip = ipaddress.IPv4Address(address)
    if (ip.is_unspecified or ip.is_loopback or ip.is_link_local or ip.is_multicast
            or not port.isdecimal() or not 1 <= int(port) <= 65535
            or port != defaults.get("ENDLESSNET_CLIENT_CONNECTION_LISTEN_PORT")):
        raise ValueError("Expected actual IPv4 endpoint using producer listen port")
    if not re.fullmatch(r"S-1-(?:[0-9]+-)*[0-9]+", sid):
        raise ValueError("Expected Windows caller SID")
    origin = "https://" + url.hostname
    if origin == "https://endlessnet.ru":
        origin = "https://api.endlessnet.ru"
    result = dict(defaults)
    result.update({"ENDLESSNET_CLIENT_CONTROL_ORIGIN": origin,
                   "ENDLESSNET_CLIENT_APPROVAL_BASE": ("https://admin.endlessnet.ru/" if origin == "https://api.endlessnet.ru" else origin + "/"),
                   "ENDLESSNET_CLIENT_DEFAULT_PROFILE_ID": str(uuid.uuid5(uuid.NAMESPACE_URL, origin)),
                   "ENDLESSNET_CLIENT_INSTALLATION_ID": str(uuid.uuid4()),
                   "ENDLESSNET_CLIENT_SERVICE_SID": "S-1-5-18",
                   "ENDLESSNET_CLIENT_ALLOWED_SIDS": sid,
                   "ENDLESSNET_CLIENT_STATE_DIRECTORY": state_directory,
                   "ENDLESSNET_CLIENT_CONNECTION_ENDPOINT": endpoint})
    return result


def literal(value):
    return re.sub(r"[\[\]]", lambda m: "[\\" + m[0] + "]", value.replace("$", "$$"))


def render(bundle, cli, service, actions, version, output):
    if not re.fullmatch(r"v?0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError("Expected v0.MINOR.PATCH")
    parts = [int(x) for x in version.removeprefix("v").split(".")]
    if parts[1] > 255 or parts[2] > 65535:
        raise ValueError("Version exceeds Windows Installer limits")
    bundle, cli, actions, output = map(lambda p: Path(p).resolve(), (bundle, cli, actions, output))
    files = {name: cli if name == "endlessnet.exe" else bundle / name for name in PAYLOAD}
    if not actions.is_file() or any(not p.is_file() or p.is_symlink() for p in files.values()):
        raise ValueError("Missing regular Client/CLI payload or native actions")
    if not service or any(not isinstance(k, str) or not re.fullmatch(r"ENDLESSNET_CLIENT_[A-Z0-9_]+", k)
                          or not isinstance(v, str) or any(c in v for c in "\0\r\n")
                          for k, v in service.items()):
        raise ValueError("Invalid Client configuration")
    port = service.get("ENDLESSNET_CLIENT_CONNECTION_LISTEN_PORT", "")
    if not port.isdecimal() or not 1 <= int(port) <= 65535:
        raise ValueError("Invalid producer listen port")
    output.mkdir(parents=True, exist_ok=True)
    ET.register_namespace("", NS)
    root = ET.Element("{" + NS + "}Wix")

    def add(parent, tag, **attrs):
        return ET.SubElement(parent, "{" + NS + "}" + tag, attrs)

    package = add(root, "Package", Name="EndlessNet Client", Manufacturer="EndlessNet",
                  Version=version.removeprefix("v"), UpgradeCode=UPGRADE_CODE,
                  Scope="perMachine", InstallerVersion="500", Compressed="yes")
    add(package, "MajorUpgrade", Schedule="afterInstallInitialize", DowngradeErrorMessage="Установлена более новая версия EndlessNet Client.")
    add(package, "MediaTemplate", EmbedCab="yes")
    related = add(package, "Upgrade", Id=COMBINED_UPGRADE_CODE)
    add(related, "UpgradeVersion", Minimum="0.0.0", IncludeMinimum="yes", OnlyDetect="yes", Property="ENDLESSNET_COMBINED_PRODUCT")
    add(package, "Launch", Condition="Installed OR NOT ENDLESSNET_COMBINED_PRODUCT",
        Message="Сначала удалите общую установку EndlessNet через Windows. Данные клиента сохраняются; параллельная установка одной службы запрещена.")
    for name in ("ENDLESSNET_INSTALLATION_ID", "ENDLESSNET_EXISTING_INSTALLATION", "ENDLESSNET_LOCAL_ENDPOINT"):
        add(package, "Property", Id=name, Secure="yes")
    add(package, "Property", Id="EndlessNetListenPort", Value=port)
    app = add(add(package, "StandardDirectory", Id="ProgramFiles64Folder"), "Directory", Id="AppRoot", Name="EndlessNet")
    vendor = add(add(package, "StandardDirectory", Id="CommonAppDataFolder"), "Directory", Id="DataVendor", Name="EndlessNet")
    data = add(vendor, "Directory", Id="ClientData", Name="Client")
    state = add(data, "Directory", Id="ClientState", Name="state")
    feature = add(package, "Feature", Id="Client", Title="Client service and CLI", Level="1")

    def component(directory, name, permanent=False, never_overwrite=False):
        cid = "C" + uuid.uuid5(uuid.UUID(UPGRADE_CODE.strip("{}")), name).hex
        node = add(directory, "Component", Id=cid, Guid=str(uuid.uuid5(uuid.UUID(UPGRADE_CODE.strip("{}")), name)), Bitness="always64")
        if permanent:
            node.set("Permanent", "yes")
        if never_overwrite:
            node.set("NeverOverwrite", "yes")
        add(feature, "ComponentRef", Id=cid)
        return node

    for directory, name, sddl in ((app, "app", PUBLIC), (vendor, "vendor", CONTAINER), (data, "data", PUBLIC), (state, "state", PRIVATE)):
        node = component(directory, "directory:" + name, permanent=name != "app")
        add(add(node, "CreateFolder"), "PermissionEx", Sddl=sddl)
    for name, source in files.items():
        node = component(app, name)
        fid = "ClientExe" if name == "client.exe" else "CliExe" if name == "endlessnet.exe" else "F" + uuid.uuid5(uuid.NAMESPACE_URL, name).hex
        add(add(node, "File", Id=fid, Source=str(source), KeyPath="yes"), "PermissionEx", Sddl=PUBLIC)
        if name == "client.exe":
            add(node, "ServiceInstall", Id="ClientService", Name="endlessnet-client", DisplayName="EndlessNet Client", Type="ownProcess", Start="auto", ErrorControl="normal", Account="LocalSystem", Arguments="installed-run")
            add(node, "ServiceControl", Id="ClientControl", Name="endlessnet-client", Start="install", Stop="both", Remove="uninstall", Wait="yes")
        if name == "endlessnet.exe":
            add(node, "Environment", Id="CliPath", Name="PATH", Value="[AppRoot]", Action="set", Part="last", System="yes")
    settings = {key: literal(value) for key, value in service.items()}
    settings.update({"ENDLESSNET_CLIENT_INSTALLATION_ID": "[ENDLESSNET_INSTALLATION_ID]",
                     "ENDLESSNET_CLIENT_ALLOWED_SIDS": "[UserSID]",
                     "ENDLESSNET_CLIENT_CONNECTION_ENDPOINT": "[ENDLESSNET_LOCAL_ENDPOINT]",
                     "ENDLESSNET_CLIENT_SERVICE_SID": "S-1-5-18",
                     "ENDLESSNET_CLIENT_STATE_DIRECTORY": "[CommonAppDataFolder]EndlessNet\\Client\\state",
                     "ENDLESSNET_CLIENT_RUNTIME_INSTANCE_FILE": "[CommonAppDataFolder]EndlessNet\\Client\\runtime-instance"})
    # SCM removes its key with the service. The preflight captures existing
    # settings before MajorUpgrade removes that service, then MSI writes them
    # into the new service rather than trusting a caller-supplied property.
    node = component(data, "service-configuration")
    env = add(node, "RegistryValue", Root="HKLM", Key=SERVICE, Name="Environment", Type="multiString", KeyPath="yes")
    for key, value in sorted(settings.items()):
        prop = "ENSET_" + key
        add(package, "Property", Id=prop, Value=value, Secure="yes", Hidden="yes")
        add(env, "MultiStringValue", Value=key + "=[" + prop + "]")
    node = component(data, "installation-id", permanent=True, never_overwrite=True)
    add(node, "RegistryValue", Root="HKLM", Key=r"SOFTWARE\EndlessNet\Client", Name="InstallationId", Type="string", Value="[ENDLESSNET_INSTALLATION_ID]", KeyPath="yes")
    add(package, "Binary", Id="InstallerActions", SourceFile=str(actions))
    add(package, "CustomAction", Id="PrepareMachine", BinaryRef="InstallerActions", DllEntry="PrepareMachine", Return="check")
    add(package, "CustomAction", Id="InitializeState", FileRef="ClientExe", ExeCommand="installed-init", Execute="deferred", Impersonate="no", Return="check")
    sequence = add(package, "InstallExecuteSequence")
    add(sequence, "Custom", Action="PrepareMachine", After="CostFinalize", Condition="NOT REMOVE")
    add(sequence, "WriteRegistryValues", Sequence="5810")
    add(sequence, "Custom", Action="InitializeState", After="WriteRegistryValues", Condition="NOT Installed AND NOT ENDLESSNET_EXISTING_INSTALLATION AND NOT REMOVE")
    filename = output / "EndlessNetClient.wxs"
    ET.ElementTree(root).write(filename, encoding="utf-8", xml_declaration=True)
    return filename


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    prepare = sub.add_parser("configure")
    for name in ("defaults", "server", "endpoint", "sid", "state-directory", "output"):
        prepare.add_argument("--" + name, required=True)
    source = sub.add_parser("render")
    for name in ("bundle", "cli", "service-profile", "actions", "version", "output"):
        source.add_argument("--" + name, required=True)
    args = parser.parse_args()
    if args.command == "configure":
        values = configuration(json.loads(Path(args.defaults).read_text(encoding="utf-8")), args.server,
                               args.endpoint, args.sid, args.state_directory)
        Path(args.output).write_text(json.dumps(values, ensure_ascii=False), encoding="utf-8")
    else:
        render(args.bundle, args.cli, json.loads(Path(args.service_profile).read_text(encoding="utf-8")), args.actions, args.version, args.output)


if __name__ == "__main__":
    main()
