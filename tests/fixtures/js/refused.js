const denied = ["Date","Atomics","SharedArrayBuffer","Worker","std","os",
  "require","Promise","WeakRef","FinalizationRegistry","fs","read","write",
  "fetch","WebSocket","setTimeout","performance","document","window"];
for (const name of denied) {
  print(name, typeof globalThis[name]);
  try { globalThis[name](); print("AUTHORITY-LEAK", name); }
  catch(e) { print(name, e.name); }
}
