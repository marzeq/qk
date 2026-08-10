#!/usr/bin/env python3
"""Generate the complete QK module from rlparser's Raylib API JSON output."""

import json
import re
import sys
from pathlib import Path


Reserved = {
    "alias", "as", "break", "case", "comptime", "continue", "defer", "else",
    "enum", "false", "for", "if", "import", "in", "let", "match",
    "module", "mut", "nil", "not", "opaque", "or", "pub", "return",
    "sizeof", "struct", "trait", "true", "type", "union", "when",
    "while",
}

Primitives = {
    "void": "void",
    "bool": "bool",
    "char": "std.ctypes.char",
    "unsigned char": "std.ctypes.unsigned_char",
    "unsigned short": "std.ctypes.unsigned_short",
    "unsigned int": "std.ctypes.unsigned_int",
    "int": "std.ctypes.int",
    "long": "std.ctypes.long",
    "float": "std.ctypes.float",
    "double": "std.ctypes.double",
    "va_list": "VaList",
}

EnumPrefixes = {
    "ConfigFlags": "FLAG_",
    "TraceLogLevel": "LOG_",
    "KeyboardKey": "KEY_",
    "MouseButton": "MOUSE_BUTTON_",
    "MouseCursor": "MOUSE_CURSOR_",
    "GamepadButton": "GAMEPAD_BUTTON_",
    "GamepadAxis": "GAMEPAD_AXIS_",
    "MaterialMapIndex": "MATERIAL_MAP_",
    "ShaderLocationIndex": "SHADER_LOC_",
    "ShaderUniformDataType": "SHADER_UNIFORM_",
    "ShaderAttributeDataType": "SHADER_ATTRIB_",
    "PixelFormat": "PIXELFORMAT_",
    "TextureFilter": "TEXTURE_FILTER_",
    "TextureWrap": "TEXTURE_WRAP_",
    "CubemapLayout": "CUBEMAP_LAYOUT_",
    "FontType": "FONT_",
    "BlendMode": "BLEND_",
    "Gesture": "GESTURE_",
    "CameraMode": "CAMERA_",
    "CameraProjection": "CAMERA_",
    "NPatchLayout": "NPATCH_",
}


def snake(name: str) -> str:
    name = re.sub(r"(.)([A-Z][a-z]+)", r"\1_\2", name)
    name = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", name)
    return name.lower()


def identifier(name: str) -> str:
    name = snake(name)
    return name + "_value" if name in Reserved else name


def enum_member(enum_name: str, raw: str) -> str:
    prefix = EnumPrefixes.get(enum_name, "")
    if prefix and raw.startswith(prefix):
        raw = raw[len(prefix):]
    parts = raw.split("_")
    result = ""
    for part in parts:
        if any(ch.isdigit() for ch in part):
            result += part[0].upper() + part[1:]
        else:
            result += part.capitalize()
    if result and result[0].isdigit():
        result = "Value" + result
    return result


def qk_type(c_type: str, field: bool = False) -> str:
    c_type = " ".join(c_type.strip().split())
    array = re.fullmatch(r"(.+?)\[(\d+)\]", c_type)
    if array:
        return f"[{array[2]}]{qk_type(array[1])}"

    if c_type == "...":
        return "..."
    if c_type == "const char *":
        return "cstr"
    if c_type == "const char **":
        return "*mut cstr"

    stars = c_type.count("*")
    base = c_type.replace("*", "").strip()
    is_const = base.startswith("const ")
    if is_const:
        base = base[6:].strip()
    mapped = Primitives.get(base, base)

    if stars == 0:
        return mapped

    pointer = ("*" if is_const else "*mut ") + mapped
    for _ in range(stars - 1):
        pointer = "*mut " + pointer
    return pointer


ParamTypeOverrides = {
    ("SetConfigFlags", "flags"): "ConfigFlags",
    ("IsWindowState", "flag"): "ConfigFlags",
    ("SetWindowState", "flags"): "ConfigFlags",
    ("ClearWindowState", "flags"): "ConfigFlags",
    ("SetTraceLogLevel", "logLevel"): "TraceLogLevel",
    ("TraceLog", "logLevel"): "TraceLogLevel",
    ("SetExitKey", "key"): "KeyboardKey",
    ("IsKeyPressed", "key"): "KeyboardKey",
    ("IsKeyPressedRepeat", "key"): "KeyboardKey",
    ("IsKeyDown", "key"): "KeyboardKey",
    ("IsKeyReleased", "key"): "KeyboardKey",
    ("IsKeyUp", "key"): "KeyboardKey",
    ("IsMouseButtonPressed", "button"): "MouseButton",
    ("IsMouseButtonDown", "button"): "MouseButton",
    ("IsMouseButtonReleased", "button"): "MouseButton",
    ("IsMouseButtonUp", "button"): "MouseButton",
    ("SetMouseCursor", "cursor"): "MouseCursor",
    ("IsGamepadButtonPressed", "button"): "GamepadButton",
    ("IsGamepadButtonDown", "button"): "GamepadButton",
    ("IsGamepadButtonReleased", "button"): "GamepadButton",
    ("IsGamepadButtonUp", "button"): "GamepadButton",
    ("GetGamepadAxisMovement", "axis"): "GamepadAxis",
    ("BeginBlendMode", "mode"): "BlendMode",
    ("SetGesturesEnabled", "flags"): "Gesture",
    ("IsGestureDetected", "gesture"): "Gesture",
    ("UpdateCamera", "mode"): "CameraMode",
    ("LoadTextureCubemap", "layout"): "CubemapLayout",
    ("SetTextureFilter", "filter"): "TextureFilter",
    ("SetTextureWrap", "wrap"): "TextureWrap",
    ("LoadFontData", "type"): "FontType",
    ("SetShaderValue", "uniformType"): "ShaderUniformDataType",
    ("SetShaderValueV", "uniformType"): "ShaderUniformDataType",
    ("SetMaterialTexture", "mapType"): "MaterialMapIndex",
}

ReturnTypeOverrides = {
    "GetKeyPressed": "KeyboardKey",
}


def declaration_params(symbol: str, params: list[dict]) -> str:
    rendered = []
    for param in params:
        if param["type"] == "...":
            rendered.append("...")
        else:
            rendered_type = ParamTypeOverrides.get(
                (symbol, param["name"]), qk_type(param["type"])
            )
            rendered.append(f"{identifier(param['name'])}: {rendered_type}")
    return ", ".join(rendered)


def generate(api: dict) -> str:
    out = [
        "// Generated by generate_bindings.py from Raylib's rlparser JSON output.",
        "// Do not edit this file directly.",
        "module vendor.raylib",
        "  @link(",
        '    system "c",',
        "    when OS == .Linux && Arch == .X86_64 {",
        '      system "X11",',
        '      system "GL",',
        '      system "m",',
        '      system "pthread",',
        '      system "dl",',
        '      system "rt",',
        '      path "./native/linux-x86_64/libraylib.a",',
        "    } else when OS == .Windows && Arch == .X86_64 {",
        '      system "gdi32",',
        '      system "winmm",',
        '      system "opengl32",',
        '      path "./native/windows-x86_64/libraylib.a",',
        "    } else when OS == .MacOS && (Arch == .AArch64 || Arch == .X86_64) {",
        '      framework "Cocoa",',
        '      framework "IOKit",',
        '      framework "CoreVideo",',
        '      framework "OpenGL",',
        '      path "./native/macos-universal/libraylib.a",',
        "    } else {",
        '      compiler_error("Cannot build raylib for this platform.")',
        "    }",
        "  )",
        "",
        "import std.ctypes",
        "",
        "pub let VaList = type alias *mut void",
        "",
        "pub let rAudioBuffer = type opaque",
        "pub let rAudioProcessor = type opaque",
        "",
    ]

    for struct in api["structs"]:
        name = struct["name"]
        out.append(f"pub let {name} = type struct {{")
        for item in struct["fields"]:
            out.append(f"  {identifier(item['name'])}: {qk_type(item['type'], field=True)},")
        out += ["}", ""]

    aliases = list(api["aliases"])
    for alias in aliases:
        name = alias["name"]
        target = alias["type"]
        if name.startswith("*"):
            name = name[1:]
            rendered_target = "*mut " + qk_type(target)
        else:
            rendered_target = qk_type(target)
        out += [f"pub let {name} = type alias {rendered_target}", ""]

    for callback in api["callbacks"]:
        name = callback["name"]
        params = ", ".join(
            qk_type(param["type"]) for param in callback.get("params", [])
        )
        out += [
            f"pub let {name} = type alias *({params}): {qk_type(callback['returnType'])}",
            "",
        ]

    for enum in api["enums"]:
        name = enum["name"]
        kind = "flags(u32)" if name in {"ConfigFlags", "Gesture"} else "enum"
        out.append(f"pub let {name} = type {kind} {{")
        used = set()
        for value in enum["values"]:
            member = enum_member(name, value["name"])
            if member in used:
                continue
            used.add(member)
            rendered_value = (
                hex(value["value"]) if kind.startswith("flags") else value["value"]
            )
            out.append(f"  {member} = {rendered_value},")
        out += ["}", ""]

    out += ["pub let Key = type alias KeyboardKey", ""]

    constants = [
        "pub let VersionMajor: i32 = 6",
        "pub let VersionMinor: i32 = 0",
        "pub let VersionPatch: i32 = 0",
        'pub let Version = c"6.0"',
        "pub let Pi: f32 = 3.14159265358979323846",
        "pub let Deg2Rad: f32 = Pi / 180.0",
        "pub let Rad2Deg: f32 = 180.0 / Pi",
    ]
    out += constants + [""]

    color_names = {
        "LIGHTGRAY": "LightGray", "GRAY": "Gray", "DARKGRAY": "DarkGray",
        "YELLOW": "Yellow", "GOLD": "Gold", "ORANGE": "Orange",
        "PINK": "Pink", "RED": "Red", "MAROON": "Maroon",
        "GREEN": "Green", "LIME": "Lime", "DARKGREEN": "DarkGreen",
        "SKYBLUE": "SkyBlue", "BLUE": "Blue", "DARKBLUE": "DarkBlue",
        "PURPLE": "Purple", "VIOLET": "Violet", "DARKPURPLE": "DarkPurple",
        "BEIGE": "Beige", "BROWN": "Brown", "DARKBROWN": "DarkBrown",
        "WHITE": "White", "BLACK": "Black", "BLANK": "Blank",
        "MAGENTA": "Magenta", "RAYWHITE": "RayWhite",
    }
    color_defines = {item["name"]: item for item in api["defines"] if item["type"] == "COLOR"}
    for c_name, qk_name in color_names.items():
        values = [int(value) for value in re.findall(r"\d+", color_defines[c_name]["value"])]
        out += [
            f"pub let {qk_name} = Color.{{",
            f"  r = {values[0]}, g = {values[1]}, b = {values[2]}, a = {values[3]},",
            "}",
        ]
    out += [""]

    for function in api["functions"]:
        symbol = function["name"]
        params = declaration_params(symbol, function.get("params", []))
        return_type = ReturnTypeOverrides.get(symbol, qk_type(function["returnType"]))
        out.append(
            f"pub let {snake(symbol)}({params}): {return_type} "
            f"@foreign(symbol \"{symbol}\")"
        )

    out += [
        "",
        "pub let get_mouse_ray = get_screen_to_world_ray",
    ]

    out += [""]
    return "\n".join(out)


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("usage: generate_bindings.py API.json raylib.qks")
    api_path, binding_path = map(Path, sys.argv[1:])
    api = json.loads(api_path.read_text())
    binding_path.write_text(generate(api))


if __name__ == "__main__":
    main()
