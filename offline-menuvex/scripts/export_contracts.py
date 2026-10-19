"""Run from repository root with the backend Python environment; no DB connection needed."""
from __future__ import annotations
import collections
import json
import os
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
os.environ.setdefault("DATABASE_URL", "postgresql+asyncpg://unused:unused@localhost:5432/unused")
os.environ.setdefault("SECRET_KEY", "contract-export-only-not-for-server-use")
from app.main import app
from fastapi.routing import APIRoute, APIWebSocketRoute

OUT = ROOT / "desktop/contracts"
METHODS = {"get", "post", "put", "patch", "delete", "head", "options"}

def write(name, value):
    (OUT / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")

def dependencies(dependant):
    names = set()
    for dep in dependant.dependencies:
        name = getattr(dep.call, "__name__", None) or type(dep.call).__name__
        names.add(name)
        names.update(dependencies(dep))
    return sorted(names)

spec = app.openapi()
write("openapi.json", spec)
routes = []
for route in app.routes:
    if isinstance(route, APIRoute):
        for method in sorted(route.methods):
            routes.append({"method": method, "path": route.path, "in_schema": route.include_in_schema,
                           "name": route.name, "dependencies": dependencies(route.dependant)})
    elif isinstance(route, APIWebSocketRoute):
        routes.append({"method": "WS", "path": route.path, "name": route.name,
                       "dependencies": dependencies(route.dependant)})
write("registered-routes.json", routes)
ops = []
for path, item in spec["paths"].items():
    for method, op in item.items():
        if method not in METHODS:
            continue
        ops.append({"method": method.upper(), "path": path, "summary": op.get("summary", ""),
                    "tags": op.get("tags", []), "operation_id": op.get("operationId", ""),
                    "security": op.get("security", spec.get("security", [])),
                    "parameters": op.get("parameters", []), "request_body": op.get("requestBody"),
                    "responses": op.get("responses", {})})
ops.sort(key=lambda op: (op["path"], op["method"]))
write("catalog.json", ops)
write("sync-matrix.json", json.loads((ROOT / "docs/sync/sync-matrix.json").read_text()))
# These routes are implemented by a separate Go process, not by app.openapi().
source = (ROOT / "backend/internal/syncengine/service.go").read_text()
sync_routes = [{"method": method, "path": "/api/v1/sync" + path,
                "auth": "user JWT" if group == "admin" else "device" if group == "dev" else "internal key"}
               for group, method, path in re.findall(r'(admin|dev|group)\.(GET|POST|PUT|DELETE)\("([^\"]+)"', source)]
write("go-sync-routes.json", sync_routes)
ids = collections.Counter(op["operation_id"] for op in ops if op["operation_id"])
missing_ids = [op["method"] + " " + op["path"] for op in ops if not op["operation_id"]]
def route_shape(path):
    return re.sub(r"\{[^}]+\}", "{}", path).rstrip("/") or "/"

registered = {(r["method"], route_shape(r["path"])) for r in routes}
documented = {(op["method"], route_shape(op["path"])) for op in ops}
doc_only = [op["method"] + " " + op["path"] for op in ops if (op["method"], route_shape(op["path"])) not in registered]
audit = {"documented_paths": len(spec["paths"]), "documented_operations": len(ops),
         "schemas": len(spec.get("components", {}).get("schemas", {})),
         "registered_http_operations": sum(r["method"] != "WS" for r in routes),
         "websocket_routes": [r for r in routes if r["method"] == "WS"],
         "documented_not_registered_in_fastapi": doc_only,
         "registered_not_documented": [r["method"] + " " + r["path"] for r in routes
             if r["method"] != "WS" and (r["method"], route_shape(r["path"])) not in documented],
         "comparison_note": "Route shapes normalize path parameter names and trailing slashes; raw aliases remain in registered-routes.json.",
         "missing_operation_ids": missing_ids,
         "duplicate_operation_ids": [key for key, count in ids.items() if count > 1],
         "operations_by_tag": dict(sorted(collections.Counter(tag for op in ops for tag in op["tags"] or ["untagged"]).items())),
         "go_sync_operations_not_in_fastapi_schema": sync_routes}
write("audit.json", audit)
lines = ["# Generated endpoint catalog", "", "Generated from `app.openapi()`. Authentication/permissions must also be checked against registered dependencies and domain rules.", "", "| Method | Path | Tags | Summary |", "|---|---|---|---|"]
lines += [f"| {op['method']} | `{op['path']}` | {', '.join(op['tags'])} | {op['summary'].replace('|', '/')} |" for op in ops]
(OUT / "ENDPOINTS.md").write_text("\n".join(lines) + "\n")
print(json.dumps({key: audit[key] for key in ("documented_paths", "documented_operations", "schemas", "registered_http_operations")}, indent=2))

# Every FastAPI operation is mapped to its standalone offline desktop module in Coffish Desktop.
def classify_desktop_module(op):
    path = op["path"]
    tags = set(op.get("tags") or [])
    if tags & {"POS", "Orders", "Orders Streaming", "Split bill"} or "/pos" in path or "/orders" in path or "/split-bill" in path:
        return ("pos_orders_splitbill", "صندوق فروش (POS)، مدیریت سفارش‌ها، تقسیم دنگ (Split Bill) و بستن شیفت روزانه در پایگاه داده محلی دسکتاپ.")
    if tags & {"Categories", "Products", "Modifier Groups", "Banners", "Themes", "Public Menu"} or any(p in path for p in ("/categories", "/products", "/modifier-groups", "/banners", "/themes", "/menu")):
        return ("menu_catalog_modifiers", "مدیریت منو، محصولات، دسته‌بندی‌ها، افزودنی‌ها (Modifiers)، تم منوی دیجیتال و بنرها به‌صورت کاملاً آفلاین.")
    if tags & {"Inventory", "Inventory Suppliers", "Inventory Purchases"} or "/inventory" in path:
        return ("inventory_suppliers_bom", "انبارداری، مواد اولیه، رسپی (BOM)، تامین‌کنندگان، فاکتورهای خرید، ضایعات و برگه‌های شمارش انبارگردانی.")
    if tags & {"Tables"} or "/tables" in path:
        return ("tables_floor_reservations", "مدیریت سالن، نقشه میزها، انتقال میز و تقویم رزرواسیون مهمانان.")
    if tags & {"Customers & Customer Club", "Customer retention", "Online ordering", "Online customer tickets"} or any(p in path for p in ("/customers", "/retention", "/online")):
        return ("customers_loyalty_retention", "باشگاه مشتریان، سطوح وفاداری، کمپین‌های بازگشت مشتری (Retention)، پلن‌های اشتراک کافه و سفارش‌های آنلاین.")
    if tags & {"Debtors"} or "/debtors" in path:
        return ("debtors_credit_ledger", "دفتر حساب‌های دفتری، نسیه مشتریان و تسویه اقساطی یا کامل مطالبات.")
    if tags & {"Accounting", "Accounting & Reports", "Deep analytics", "Analytics", "Dashboard"} or any(p in path for p in ("/accounting", "/deep-analytics", "/analytics", "/reports", "/dashboard", "/expenses")):
        return ("accounting_finance_analytics", "حسابداری دوبل، اسناد مالی، ثبت هزینه‌ها، تراز آزمایشی، صورت سود و زیان و تحلیل عمیق فروش (Deep Analytics).")
    if tags & {"Staff", "Jobs - employer", "Jobs - candidate", "Jobs - public"} or any(p in path for p in ("/staff", "/jobs", "/tasks")):
        return ("staff_shifts_jobs", "مدیریت پرسنل، شیفت‌ها، چک‌لیست اجرایی روزانه و سامانه استخدام و کاریابی باریستا/صندوقدار.")
    if tags & {"Printers", "Printer Configurations", "Printer Category Routes", "POS Print Jobs"} or any(p in path for p in ("/printer", "/print-jobs")):
        return ("thermal_printers_routing", "چاپگرهای حرارتی ۸۰ میلی‌متری، مسیردهی دسته‌بندی به چاپگر و صدور فیش مشتری، حواله بار و فیش باشگاه مشتریان.")
    if tags & {"Cafe Profile", "Settings", "Quick Start", "Business Types", "Alerts", "Tickets", "Updates", "Subscription plans", "Subscription billing — cafe", "Subscription billing — public", "Subscription billing — payments"} or any(p in path for p in ("/cafe", "/settings", "/working-hours", "/quick-start", "/alerts", "/tickets", "/updates", "/business-types", "/subscription")):
        return ("cafe_profile_settings", "تنظیمات پروفایل کافه، ساعات کاری، راه‌اندازی سریع، اعلان‌ها، تیکت‌ها و پشتیبان‌گیری محلی.")
    return ("local_system_governance", "مدیریت حساب کاربری محلی، سطوح دسترسی، وضعیت سلامت هسته دسکتاپ و تنظیمات سیستمی.")

capabilities = []
for op in ops:
    mode, note = classify_desktop_module(op)
    capabilities.append({
        "method": op["method"],
        "path": op["path"],
        "mode": mode,
        "note": note,
    })
write("desktop-capabilities.json", capabilities)

# Resolve every local schema reference and check path parameters, not merely counts.
references = 0
unresolved = set()
def walk(value):
    global references
    if isinstance(value, dict):
        ref = value.get("$ref")
        if isinstance(ref, str):
            references += 1
            if ref.startswith("#/"):
                current = spec
                try:
                    for key in ref[2:].split("/"):
                        current = current[key.replace("~1", "/").replace("~0", "~")]
                except (KeyError, TypeError):
                    unresolved.add(ref)
        for child in value.values():
            walk(child)
    elif isinstance(value, list):
        for child in value:
            walk(child)
walk(spec)
parameter_gaps = []
for op in ops:
    expected = set(re.findall(r"\{([^}]+)\}", op["path"]))
    actual = {p.get("name") for p in op["parameters"] if p.get("in") == "path" and p.get("required")}
    if expected != actual:
        parameter_gaps.append({"operation": op["method"] + " " + op["path"], "expected": sorted(expected), "declared_required": sorted(actual)})
write("validation.json", {"references_checked": references, "unresolved_local_refs": sorted(unresolved),
                          "path_parameter_mismatches": parameter_gaps,
                          "operation_dispositions": len(capabilities),
                          "dispositions_by_mode": dict(collections.Counter(c["mode"] for c in capabilities))})
