// report.js — the desk's first shipped scripted workflow
// (card d / index #3334, issue #3441). Reads the staged app manifest
// /host/APPS.TXT and writes a summary to /host/APPSUM.TXT through the
// virelai namespace.
//
// Run from the shell:   exec QJS.BIN /host/SCRIPTS/REPORT.JS
// Or press Enter on REPORT.JS in the file manager (application/javascript).
//
// Honest limits: synchronous only; paths confined to /host/ (64 B total);
// each call moves at most 262144 bytes; writeFile is exclusive-create, so
// re-running while APPSUM.TXT exists refuses PathAlreadyExists — JS has no
// delete or rename verb to clear it.

// Product version — the `ver M.m.p` the component-registry row shows
// and the drift check resolves (docs/component-registry.md); bump per
// the registry's rule. Bare assignment is deliberate: the check matches
// a `VER = "M.m.p"` line, and this file is a classic non-strict script.
VER = "1.0.0"

var text = virelai.readFile('/host/APPS.TXT');
var lines = text.split('\n');
var rows = 0;
var docked = 0;
var names = [];
for (var i = 0; i < lines.length; i++) {
    var line = lines[i].trim();
    if (line === '' || line[0] === '#') continue;
    var f = line.split('|');
    if (f.length < 4) continue;
    rows++;
    if (f[3].trim() === 'dock=true') docked++;
    names.push(f[1].trim());
}
var out = 'APPS.TXT summary\n' +
    'apps: ' + rows + ' (' + docked + ' docked)\n' +
    names.join('\n') + '\n';
try {
    virelai.writeFile('/host/APPSUM.TXT', out);
} catch (e) {
    print('report: refused ' + e.name);
    throw e;
}
print('report: apps=' + rows + ' docked=' + docked + ' wrote /host/APPSUM.TXT');
