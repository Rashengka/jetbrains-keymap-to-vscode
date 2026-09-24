// Generates internal/layout/mac.json from the keyboard layouts installed in macOS.
//
//   swift tools/gen-mac-layouts.swift > internal/layout/mac.json
//
// For every character key it records what the key types without modifiers and
// with Shift. Keys are named like KeyboardEvent.code, which is what VS Code
// uses for scan-code keybindings such as "cmd+[Digit2]". Dead keys are recorded
// as the diacritic they type on their own (for example U+00A8 for a dead diaeresis).
import Carbon
import Foundation

let layouts = [
    "com.apple.keylayout.Czech",
    "com.apple.keylayout.Czech-QWERTY",
    "com.apple.keylayout.Slovak",
    "com.apple.keylayout.Slovak-QWERTY",
]

// macOS virtual key code -> KeyboardEvent.code (as mapped by Chromium, which VS Code runs on).
let codes: [UInt16: String] = [
    0x00: "KeyA", 0x01: "KeyS", 0x02: "KeyD", 0x03: "KeyF", 0x04: "KeyH", 0x05: "KeyG",
    0x06: "KeyZ", 0x07: "KeyX", 0x08: "KeyC", 0x09: "KeyV", 0x0A: "IntlBackslash", 0x0B: "KeyB",
    0x0C: "KeyQ", 0x0D: "KeyW", 0x0E: "KeyE", 0x0F: "KeyR", 0x10: "KeyY", 0x11: "KeyT",
    0x12: "Digit1", 0x13: "Digit2", 0x14: "Digit3", 0x15: "Digit4", 0x16: "Digit6", 0x17: "Digit5",
    0x18: "Equal", 0x19: "Digit9", 0x1A: "Digit7", 0x1B: "Minus", 0x1C: "Digit8", 0x1D: "Digit0",
    0x1E: "BracketRight", 0x1F: "KeyO", 0x20: "KeyU", 0x21: "BracketLeft", 0x22: "KeyI", 0x23: "KeyP",
    0x25: "KeyL", 0x26: "KeyJ", 0x27: "Quote", 0x28: "KeyK", 0x29: "Semicolon", 0x2A: "Backslash",
    0x2B: "Comma", 0x2C: "Slash", 0x2D: "KeyN", 0x2E: "KeyM", 0x2F: "Period", 0x32: "Backquote",
]

func translate(_ layout: UnsafePointer<UCKeyboardLayout>, _ key: UInt16, shift: Bool) -> String {
    var dead: UInt32 = 0
    var length = 0
    var chars = [UniChar](repeating: 0, count: 8)
    let modifiers: UInt32 = shift ? UInt32((shiftKey >> 8) & 0xFF) : 0
    let status = UCKeyTranslate(layout, key, UInt16(kUCKeyActionDown), modifiers, UInt32(LMGetKbdType()),
                                OptionBits(kUCKeyTranslateNoDeadKeysMask), &dead, chars.count, &length, &chars)
    return status == noErr ? String(utf16CodeUnits: chars, count: length) : ""
}

var out: [String: [String: [String]]] = [:]
for id in layouts {
    let filter = [kTISPropertyInputSourceID as String: id] as CFDictionary
    guard let list = TISCreateInputSourceList(filter, true)?.takeRetainedValue() as? [TISInputSource],
          let source = list.first,
          let ptr = TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData) else {
        FileHandle.standardError.write("layout not found: \(id)\n".data(using: .utf8)!)
        exit(1)
    }
    let data = Unmanaged<CFData>.fromOpaque(ptr).takeUnretainedValue() as Data
    var keys: [String: [String]] = [:]
    data.withUnsafeBytes { raw in
        let layout = raw.bindMemory(to: UCKeyboardLayout.self).baseAddress!
        for (key, code) in codes {
            keys[code] = [translate(layout, key, shift: false), translate(layout, key, shift: true)]
        }
    }
    out[id] = keys
}
let json = try JSONSerialization.data(withJSONObject: out, options: [.prettyPrinted, .sortedKeys])
print(String(data: json, encoding: .utf8)!)
