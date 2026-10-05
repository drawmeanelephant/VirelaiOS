// Host-only ADR 0040 A2.1 outside reference. No guest code or external packages.
import CoreGraphics
import Darwin
import Foundation

enum RenderError: Error {
    case input, geometry, context, alpha
}

func render(_ input: String, _ output: String, _ number: Int) throws {
    guard let document = CGPDFDocument(URL(fileURLWithPath: input) as CFURL),
          let page = document.page(at: number) else {
        throw RenderError.input
    }
    let box = page.getBoxRect(.mediaBox).intersection(page.getBoxRect(.cropBox))
    let scale: CGFloat = 96.0 / 72.0
    let pixelWidth = box.width * scale
    let pixelHeight = box.height * scale
    guard !box.isNull, !box.isEmpty, page.rotationAngle == 0,
          pixelWidth.isFinite, pixelHeight.isFinite,
          pixelWidth > 0, pixelWidth <= 1024, pixelHeight > 0, pixelHeight <= 1536,
          pixelWidth == pixelWidth.rounded(.towardZero),
          pixelHeight == pixelHeight.rounded(.towardZero) else {
        throw RenderError.geometry
    }
    let width = Int(pixelWidth), height = Int(pixelHeight)
    var pixels = [UInt8](repeating: 0, count: width * height * 4)
    try pixels.withUnsafeMutableBytes { storage in
        guard let context = CGContext(
            data: storage.baseAddress, width: width, height: height,
            bitsPerComponent: 8, bytesPerRow: width * 4,
            space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGBitmapInfo.byteOrder32Little.rawValue
                | CGImageAlphaInfo.premultipliedFirst.rawValue
        ) else {
            throw RenderError.context
        }
        context.setFillColor(red: 1, green: 1, blue: 1, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        context.setAllowsAntialiasing(true)
        context.setShouldAntialias(true)
        context.interpolationQuality = .none
        context.scaleBy(x: scale, y: scale)
        context.translateBy(x: -box.minX, y: -box.minY)
        context.drawPDFPage(page)
    }
    // CoreGraphics bitmap storage is top-down; little-endian ARGB is BGRA.
    guard stride(from: 3, to: pixels.count, by: 4).allSatisfy({ pixels[$0] == 255 }) else {
        throw RenderError.alpha
    }
    var product = Data("PDF1".utf8)
    for value in [width, height, width] {
        let word = UInt32(value)
        for shift in stride(from: 0, to: 32, by: 8) {
            product.append(UInt8(truncatingIfNeeded: word >> shift))
        }
    }
    product.append(contentsOf: pixels)
    try product.write(to: URL(fileURLWithPath: output))
}

let arguments = CommandLine.arguments
guard arguments.count == 4, let number = Int(arguments[3]), number > 0 else {
    FileHandle.standardError.write(Data("usage: cgrender INPUT.pdf OUTPUT.bgra PAGE\n".utf8))
    exit(2)
}
do {
    try render(arguments[1], arguments[2], number)
} catch {
    FileHandle.standardError.write(Data("cgrender: \(error)\n".utf8))
    exit(1)
}
