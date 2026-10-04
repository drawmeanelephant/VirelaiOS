"""Stage pinned offline products; missing inputs and dependencies are failures."""
import json
import os
import re
import shutil
import subprocess
from pathlib import Path

import corpus
import oracle


def codes():
    text = (corpus.ROOT/"user/go/pdf/pdf.go").read_text()
    names = re.search(r"const \(\s*OK Code = iota(.*?)\n\)", text, re.S).group(1)
    return {name: i for i, name in enumerate(["OK"]+re.findall(r"^\s+([A-Za-z]+)\s*$", names, re.M))}


def reclamation_dependency():
    query = '''query { repository(owner:"drawmeanelephant", name:"VirelaiOS") {
      issue(number:1945) { state closedByPullRequestsReferences(first:10) {
        nodes { merged mergeCommit { oid } } } } } }'''
    data = json.loads(subprocess.check_output(["gh", "api", "graphql", "-f", "query="+query], text=True, cwd=corpus.ROOT))
    if data.get("errors"):
        raise ValueError("M90fDependencyUnverified")
    issue = data["data"]["repository"]["issue"]
    for node in issue["closedByPullRequestsReferences"]["nodes"]:
        if node["merged"] and node["mergeCommit"] and issue["state"] == "CLOSED":
            oid = node["mergeCommit"]["oid"]
            if subprocess.run(["git", "-C", str(corpus.ROOT), "merge-base", "--is-ancestor", oid, "HEAD"]).returncode == 0:
                return oid
    raise ValueError("BLOCKED: M90fNotOnMain (final runtime/reclamation proof)")


def stage(run, *, final=True):
    root = corpus.ROOT
    out = Path(os.environ.get("PDF_PROOF_OUT", root/"artifacts/m89-acceptance/build"))
    fork = Path(os.environ.get("GO_FORK_DIR", root.parent/"go-virelai"))
    subprocess.run(["python3", str(root/"tools/pdf-engine/lock.py"), "check", str(root), str(fork),
                    str(root/"tools/pdf-engine/engine-lock.json")], check=True)
    subprocess.run(["python3", str(root/"tools/pdf-proof/lock.py"), "check", str(root)], check=True)
    frozen = oracle.check()
    elf = json.loads((out/"elf.json").read_text())
    if corpus.sha((out/"full.ELF").read_bytes()) != elf["full_sha256"] or corpus.sha((root/"tools/pdf-proof/proof-lock.json").read_bytes()) != elf["proof_lock_sha256"]:
        raise ValueError("SourceDrift: proof ELF")
    refdir = root/"artifacts/m89-acceptance/reference"
    for row in frozen["references"]:
        for suffix, key in ((".ppm", "ppm_sha256"), (".bgra", "bgra_sha256")):
            if corpus.sha((refdir/(row["id"]+suffix)).read_bytes()) != row[key]:
                raise ValueError("MissingOrDriftingReference: "+row["id"])
    # The full gate never bypasses this. A nonfinal diagnostic can stage only
    # the three independent lanes and cannot create a runtime plan.
    dependency = reclamation_dependency() if final else None
    share = run/"share"
    pdfs = share/"PDF"
    pdfs.mkdir(exist_ok=True)
    shutil.copyfile(out/"full.ELF", share/"PDFPROOF.ELF")
    manifest = json.loads((corpus.FIXTURES/"manifest.json").read_text())
    for row in manifest["accepted"]+manifest["negatives"]:
        source = corpus.FIXTURES/row["file"]
        if corpus.sha(source.read_bytes()) != row["sha256"]:
            raise ValueError("SourceDrift: authored fixture")
        shutil.copyfile(source, pdfs/row["file"])
    products = corpus.generate(root/"artifacts/m89-acceptance/generated")
    for name in products:
        shutil.copyfile(root/"artifacts/m89-acceptance/generated"/(name+".pdf"), pdfs/(name+".pdf"))
    recipe = json.loads((corpus.FIXTURES/"recipes.json").read_text())
    for row in recipe["capacities"]:
        shutil.copyfile(root/"artifacts/m89-acceptance/generated/capacity"/(row["id"]+".pdf"),
                        pdfs/("capacity-"+row["id"]+".pdf"))
    code = codes()
    plans = {"accepted": [], "refusals": [], "maxima": [], "runtime": []}
    refrows = {row["id"]: row for row in frozen["references"]}
    def append(group, name, source, expected, repeats, output=True):
        plans[group].append({"id": name, "source": source, "expected": expected, "repeats": repeats,
                             "output": f"PDF/{group}-{name}.bgra" if output and expected == "OK" else "-",
                             "reference": refrows.get(source)})
    for row in manifest["accepted"]:
        append("accepted", row["id"], row["id"], "OK", 21)
    for row in manifest["negatives"]:
        append("refusals", row["id"], "negative-"+row["id"], row["expected"], 1, False)
        append("refusals", "recovery-"+row["id"], "empty", "OK", 21)
    for name in products:
        append("maxima", name, name, "OK", 21)
    for row in recipe["capacities"]:
        append("maxima", "capacity-"+row["id"], "capacity-"+row["id"], row["expected"],
               21 if row["expected"] == "OK" else 1)
    for name, source, expected in (("success", "type3-position", "OK"), ("refusal", "negative-stroke", "UnsupportedStroke"), ("recovery", "empty", "OK")):
        append("runtime", name, source, expected, 1, False)
    if not final:
        del plans["runtime"]
    for group, rows in plans.items():
        text = "".join(f"{r['id']}\t/host/PDF/{r['source']}.pdf\t"+
                       ("/host/"+r["output"] if r["output"] != "-" else "-")+
                       f"\t0\t{code[r['expected']]}\t{r['repeats']}\n" for r in rows)
        if len(text.encode()) >= 16384:
            raise ValueError("ReceiptLimit: plan")
        (share/(group+".plan")).write_text(text)
    context = {"plans": plans, "codes": code, "elf": elf, "M90f_merge": dependency,
               "mode": "final-acceptance" if final else "nonfinal-diagnostic",
               "host_cpu": subprocess.check_output(["sysctl", "-n", "machdep.cpu.brand_string"], text=True).strip(),
               "host_memory": int(subprocess.check_output(["sysctl", "-n", "hw.memsize"])),
               "host_load": list(os.getloadavg()), "vm": {"vcpus": 2, "memory": 268435456},
               "guest_clock": "vi.Nanos: CNTPCT_EL0, pinned runtime CNTPCT/CNTFRQ conversion",
               "oracle": frozen["provenance"]}
    (run/"pdf-context.json").write_text(json.dumps(context, indent=2)+"\n")
    return context
