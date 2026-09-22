import {
  Activity,
  Banknote,
  BarChart3,
  BellRing,
  Bike,
  BookOpenCheck,
  Boxes,
  Building2,
  ChevronDown,
  CircleAlert,
  CircleUserRound,
  ClipboardList,
  Command,
  FileCheck2,
  FileStack,
  FileBarChart2,
  LogOut,
  Map,
  MapPinned,
  Menu,
  HandCoins,
  KeyRound,
  Landmark,
  PackageOpen,
  PackageSearch,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  ReceiptText,
  RotateCcw,
  Route,
  ScanLine,
  Search,
  Settings,
  ShieldCheck,
  Truck,
  Users,
  WalletCards,
  Warehouse,
  Webhook,
  X,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import { signedInHome } from "../auth/navigation";
import { Button, Dialog, Input } from "../components/ui";
import { cn, initials } from "../lib/utils";

interface NavigationItem {
  label: string;
  to: string;
  icon: LucideIcon;
  permission?: string | string[];
  exact?: boolean;
}

interface NavigationGroup {
  label: string;
  items: NavigationItem[];
}

const navigation: NavigationGroup[] = [
  {
    label: "Operations",
    items: [
      {
        label: "Shipments",
        to: "/shipments",
        icon: PackageSearch,
        permission: "shipment.read",
      },
      {
        label: "New booking",
        to: "/shipments/new",
        icon: Plus,
        permission: "shipment.create",
      },
      {
        label: "Pickups",
        to: "/operations/pickups",
        icon: ClipboardList,
        permission: "pickup.read",
      },
      {
        label: "Scanner",
        to: "/operations/scanner",
        icon: ScanLine,
        permission: [
          "scan.read",
          "scan.inbound",
          "scan.outbound",
          "scan.sort",
          "scan.hold",
          "scan.exception",
        ],
      },
      {
        label: "Bags",
        to: "/operations/bags",
        icon: PackageOpen,
        permission: "bag.read",
      },
      {
        label: "Manifests",
        to: "/operations/manifests",
        icon: FileStack,
        permission: "manifest.read",
      },
      {
        label: "Trips",
        to: "/operations/trips",
        icon: Truck,
        permission: "trip.read",
      },
      {
        label: "Hub operations",
        to: "/operations/hub",
        icon: Warehouse,
        permission: ["hub.dashboard", "portal.console"],
      },
      {
        label: "Command centre",
        to: "/command-centre",
        icon: BarChart3,
        permission: "command.read",
      },
      {
        label: "Destination",
        to: "/operations/destination",
        icon: MapPinned,
        permission: "delivery.read",
      },
      {
        label: "Delivery",
        to: "/operations/delivery",
        icon: Bike,
        permission: "delivery.read",
      },
      {
        label: "NDR",
        to: "/operations/ndr",
        icon: CircleAlert,
        permission: "ndr.read",
      },
      {
        label: "RTO",
        to: "/operations/rto",
        icon: RotateCcw,
        permission: "rto.read",
      },
      {
        label: "POD",
        to: "/operations/pod/new",
        icon: FileCheck2,
        permission: ["pod.read", "pod.submit"],
      },
    ],
  },
  {
    label: "Network",
    items: [
      {
        label: "Hubs",
        to: "/network/hubs",
        icon: Building2,
        permission: "operating_unit.read",
      },
      {
        label: "Branches",
        to: "/network/branches",
        icon: Building2,
        permission: "operating_unit.read",
      },
      {
        label: "Franchises",
        to: "/network/franchises",
        icon: Boxes,
        permission: "franchise.read",
      },
      {
        label: "Geography",
        to: "/geography/pincodes",
        icon: Map,
        permission: "pincode.read",
      },
      {
        label: "Route planner",
        to: "/routing/tester",
        icon: Route,
        permission: "serviceability.check",
      },
      {
        label: "Default routes",
        to: "/routing/routes",
        icon: MapPinned,
        permission: "route.read",
      },
      {
        label: "Route overrides",
        to: "/routing/overrides",
        icon: RotateCcw,
        permission: "route.manage",
      },
    ],
  },
  {
    label: "Commercial",
    items: [
      {
        label: "Customers",
        to: "/customers",
        icon: Users,
        permission: "customer.read",
      },
      {
        label: "Courier products",
        to: "/products",
        icon: Boxes,
        permission: "courier_service.read",
      },
      {
        label: "Pricing masters",
        to: "/pricing/masters",
        icon: MapPinned,
        permission: "rate_card.read",
      },
      {
        label: "Rate cards",
        to: "/pricing/rate-cards",
        icon: Banknote,
        permission: "rate_card.read",
      },
      {
        label: "Pricing simulator",
        to: "/pricing/simulator",
        icon: Banknote,
        permission: "pricing.quote",
      },
    ],
  },
  {
    label: "Finance",
    items: [
      {
        label: "Customer collections",
        to: "/finance/collections",
        icon: ReceiptText,
        permission: "collection.read",
      },
      {
        label: "Commission",
        to: "/finance/commission/rules",
        icon: HandCoins,
        permission: "commission.read",
      },
      {
        label: "Ledger",
        to: "/finance/ledger/accounts",
        icon: BookOpenCheck,
        permission: "ledger.read",
      },
      {
        label: "COD control",
        to: "/finance/cod",
        icon: WalletCards,
        permission: "cod.read",
      },
      {
        label: "Settlements",
        to: "/finance/settlements",
        icon: Landmark,
        permission: "settlement.read",
      },
      {
        label: "Billing",
        to: "/finance/billing/invoices",
        icon: Banknote,
        permission: "invoice.read",
      },
    ],
  },
  {
    label: "Reports",
    items: [
      {
        label: "Report centre",
        to: "/reports",
        icon: FileBarChart2,
        permission: "report.read",
      },
    ],
  },
  {
    label: "Administration",
    items: [
      {
        label: "Users",
        to: "/admin/users",
        icon: Users,
        permission: "user.read",
      },
      {
        label: "Roles & permissions",
        to: "/admin/roles",
        icon: ShieldCheck,
        permission: "user.read",
      },
      {
        label: "Organization",
        to: "/admin/organization",
        icon: Settings,
        permission: "organization.read",
      },
      {
        label: "Notification templates",
        to: "/admin/notifications/templates",
        icon: BellRing,
        permission: "notification.read",
      },
      {
        label: "Notification triggers",
        to: "/admin/notifications/triggers",
        icon: BellRing,
        permission: "notification.read",
      },
      {
        label: "Notification channels",
        to: "/admin/notifications/channels",
        icon: BellRing,
        permission: "notification.read",
      },
      {
        label: "Notification deliveries",
        to: "/admin/notifications/deliveries",
        icon: Activity,
        permission: "notification.read",
      },
      {
        label: "API credentials",
        to: "/admin/integrations/api-keys",
        icon: KeyRound,
        permission: "apikey.read",
      },
      {
        label: "Webhook endpoints",
        to: "/admin/integrations/webhooks",
        icon: Webhook,
        permission: "webhook.read",
      },
      {
        label: "Webhook deliveries",
        to: "/admin/integrations/webhooks/deliveries",
        icon: Activity,
        permission: "webhook.read",
      },
    ],
  },
];

function useBreadcrumbs() {
  const { pathname } = useLocation();
  const segments = pathname.split("/").filter(Boolean);
  return segments.map((segment, index) => ({
    label: segment
      .replaceAll("-", " ")
      .replace(/^\w/, (value) => value.toUpperCase()),
    to: `/${segments.slice(0, index + 1).join("/")}`,
  }));
}

export function AppShell() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("courier.sidebar-collapsed") === "true",
  );
  const [commandOpen, setCommandOpen] = useState(false);
  const [commandQuery, setCommandQuery] = useState("");
  const [accountOpen, setAccountOpen] = useState(false);
  const { user, hasPermission, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const breadcrumbs = useBreadcrumbs();

  const visibleNavigation = useMemo(
    () =>
      [
        ...(user?.portal?.franchise && hasPermission("portal.franchise")
          ? [
              {
                label: "Workspace",
                items: [
                  {
                    label: "Franchise dashboard",
                    to: "/portal/franchise",
                    icon: Building2,
                  },
                ],
              },
            ]
          : []),
        ...navigation,
      ]
        .map((group) => ({
          ...group,
          items: group.items.filter((item) =>
            Array.isArray(item.permission)
              ? item.permission.some((permission) => hasPermission(permission))
              : hasPermission(item.permission),
          ),
        }))
        .filter((group) => group.items.length > 0),
    [hasPermission, user?.portal?.franchise],
  );

  const commandItems = visibleNavigation
    .flatMap((group) =>
      group.items.map((item) => ({ ...item, group: group.label })),
    )
    .filter((item) =>
      item.label.toLowerCase().includes(commandQuery.toLowerCase()),
    );

  useEffect(() => setMobileOpen(false), [location.pathname]);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen(true);
      }
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === "b" &&
        hasPermission("shipment.create")
      ) {
        event.preventDefault();
        void navigate("/shipments/new");
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [hasPermission, navigate]);

  const updateCollapsed = () => {
    setCollapsed((current) => {
      localStorage.setItem("courier.sidebar-collapsed", String(!current));
      return !current;
    });
  };

  return (
    <div className="min-h-screen bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only z-[100] rounded bg-white p-3 focus:not-sr-only focus:fixed focus:left-3 focus:top-3"
      >
        Skip to content
      </a>
      {mobileOpen ? (
        <button
          className="fixed inset-0 z-30 bg-slate-950/40 lg:hidden"
          aria-label="Close navigation"
          onClick={() => setMobileOpen(false)}
        />
      ) : null}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-40 flex flex-col border-r border-emerald-950/40 bg-[#123f36] text-white transition-[width,transform] duration-200 lg:translate-x-0",
          collapsed ? "w-[76px]" : "w-[248px]",
          mobileOpen
            ? "visible translate-x-0"
            : "invisible -translate-x-full lg:visible",
        )}
        aria-label="Primary navigation"
      >
        <div className="flex h-16 items-center gap-3 border-b border-white/10 px-4">
          <Link
            to={signedInHome(user)}
            className="flex min-w-0 flex-1 items-center gap-3"
            aria-label="Ceserv home"
          >
            <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#d8f25a] text-sm font-black text-[#123f36]">
              CS
            </span>
            {!collapsed ? (
              <span className="min-w-0">
                <strong className="block truncate text-sm tracking-wide">
                  CESERV
                </strong>
                <span className="block truncate text-[10px] uppercase tracking-[0.2em] text-emerald-100/70">
                  Courier OS
                </span>
              </span>
            ) : null}
          </Link>
          <button
            className="rounded p-1 text-emerald-100 hover:bg-white/10 lg:hidden"
            onClick={() => setMobileOpen(false)}
            aria-label="Close navigation"
          >
            <X aria-hidden className="h-5 w-5" />
          </button>
        </div>
        <nav
          aria-label="Primary navigation"
          className="flex-1 overflow-y-auto px-2 py-4"
        >
          {visibleNavigation.map((group) => (
            <div key={group.label} className="mb-5">
              <p
                className={cn(
                  "mb-1 px-3 text-[10px] font-semibold uppercase tracking-[0.16em] text-emerald-100/55",
                  collapsed && "sr-only",
                )}
              >
                {group.label}
              </p>
              <div className="space-y-0.5">
                {group.items.map((item) => {
                  const Icon = item.icon;
                  return (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      title={collapsed ? item.label : undefined}
                      className={({ isActive }) =>
                        cn(
                          "flex min-h-10 items-center gap-3 rounded-md px-3 text-sm font-medium text-emerald-50/80 transition-colors hover:bg-white/10 hover:text-white",
                          isActive &&
                            "bg-white/12 text-white before:-ml-3 before:h-5 before:w-[3px] before:rounded-r before:bg-[#d8f25a]",
                          collapsed && "justify-center px-2",
                        )
                      }
                    >
                      <Icon aria-hidden className="h-4 w-4 shrink-0" />
                      {!collapsed ? <span>{item.label}</span> : null}
                    </NavLink>
                  );
                })}
              </div>
            </div>
          ))}
        </nav>
        <button
          className="mx-2 mb-3 hidden min-h-10 items-center justify-center gap-2 rounded-md text-xs text-emerald-100/70 hover:bg-white/10 hover:text-white lg:flex"
          onClick={updateCollapsed}
        >
          {collapsed ? (
            <PanelLeftOpen aria-hidden className="h-4 w-4" />
          ) : (
            <>
              <PanelLeftClose aria-hidden className="h-4 w-4" /> Collapse
              sidebar
            </>
          )}
        </button>
      </aside>
      <div
        className={cn(
          "min-h-screen transition-[padding] duration-200",
          collapsed ? "lg:pl-[76px]" : "lg:pl-[248px]",
        )}
      >
        <header className="sticky top-0 z-30 flex h-16 items-center gap-3 border-b border-border bg-white/95 px-4 backdrop-blur sm:px-6">
          <button
            className="rounded-md p-2 text-slate-600 hover:bg-muted lg:hidden"
            onClick={() => setMobileOpen(true)}
            aria-label="Open navigation"
          >
            <Menu aria-hidden className="h-5 w-5" />
          </button>
          <button
            className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-md border border-border bg-slate-50 px-3 text-left text-sm text-slate-500 hover:bg-muted sm:max-w-md"
            onClick={() => setCommandOpen(true)}
          >
            <Search aria-hidden className="h-4 w-4" />
            <span className="truncate">Search or jump to…</span>
            <kbd className="ml-auto hidden rounded border bg-white px-1.5 py-0.5 font-sans text-[10px] text-slate-500 sm:inline">
              ⌘ K
            </kbd>
          </button>
          {hasPermission("shipment.create") ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => void navigate("/shipments/new")}
              className="hidden sm:flex"
            >
              <Plus aria-hidden className="h-4 w-4" /> New booking
            </Button>
          ) : null}
          <div className="relative">
            <button
              className="flex items-center gap-2 rounded-md p-1.5 text-left hover:bg-muted"
              onClick={() => setAccountOpen((value) => !value)}
              aria-expanded={accountOpen}
              aria-haspopup="menu"
            >
              <span className="grid h-8 w-8 place-items-center rounded-full bg-emerald-100 text-xs font-bold text-emerald-900">
                {initials(user?.fullName)}
              </span>
              <span className="hidden max-w-[140px] sm:block">
                <span className="block truncate text-xs font-semibold text-slate-900">
                  {user?.fullName}
                </span>
                <span className="block truncate text-[10px] text-slate-500">
                  {user?.organization?.name}
                </span>
              </span>
              <ChevronDown
                aria-hidden
                className="hidden h-3.5 w-3.5 text-slate-400 sm:block"
              />
            </button>
            {accountOpen ? (
              <div
                role="menu"
                className="absolute right-0 top-11 w-56 rounded-lg border border-border bg-white p-1.5 shadow-overlay"
              >
                <Link
                  role="menuitem"
                  to="/profile"
                  onClick={() => setAccountOpen(false)}
                  className="flex items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted"
                >
                  <CircleUserRound aria-hidden className="h-4 w-4" /> My profile
                </Link>
                <button
                  role="menuitem"
                  onClick={() => void logout()}
                  className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm text-danger hover:bg-red-50"
                >
                  <LogOut aria-hidden className="h-4 w-4" /> Sign out
                </button>
              </div>
            ) : null}
          </div>
        </header>
        <div className="border-b border-border bg-white px-4 py-2 sm:px-6">
          <nav
            aria-label="Breadcrumb"
            className="flex min-w-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs text-slate-500"
          >
            <Link to="/shipments" className="shrink-0 hover:text-primary">
              Home
            </Link>
            {breadcrumbs.map((crumb, index) => (
              <span
                key={crumb.to}
                className="flex min-w-0 items-center gap-1.5"
              >
                <span className="shrink-0" aria-hidden>
                  /
                </span>
                {index === breadcrumbs.length - 1 ? (
                  <span
                    aria-current="page"
                    className="block min-w-0 truncate font-medium text-slate-700"
                  >
                    {crumb.label}
                  </span>
                ) : (
                  <Link
                    className="block min-w-0 truncate hover:text-primary"
                    to={crumb.to}
                  >
                    {crumb.label}
                  </Link>
                )}
              </span>
            ))}
          </nav>
        </div>
        <main
          id="main-content"
          className="mx-auto w-full min-w-0 max-w-[1680px] overflow-x-clip p-4 sm:p-6"
        >
          <Outlet />
        </main>
      </div>
      <Dialog
        open={commandOpen}
        onOpenChange={setCommandOpen}
        title="Command menu"
        description="Jump directly to an area of the courier operating system."
      >
        <div className="relative">
          <Command
            aria-hidden
            className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
          />
          <Input
            autoFocus
            value={commandQuery}
            onChange={(event) => setCommandQuery(event.target.value)}
            className="pl-9"
            placeholder="Type a page name…"
          />
        </div>
        <div className="mt-3 max-h-72 overflow-y-auto">
          {commandItems.length ? (
            commandItems.map((item) => {
              const Icon = item.icon;
              return (
                <button
                  key={item.to}
                  className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left hover:bg-muted"
                  onClick={() => {
                    void navigate(item.to);
                    setCommandOpen(false);
                    setCommandQuery("");
                  }}
                >
                  <Icon aria-hidden className="h-4 w-4 text-slate-500" />
                  <span className="flex-1 text-sm font-medium">
                    {item.label}
                  </span>
                  <span className="text-xs text-slate-400">{item.group}</span>
                </button>
              );
            })
          ) : (
            <p className="py-8 text-center text-sm text-slate-500">
              No matching page
            </p>
          )}
        </div>
      </Dialog>
    </div>
  );
}

export function ProtectedRoute({ permission }: { permission?: string }) {
  const { user, isRestoring, hasPermission } = useAuth();
  const location = useLocation();
  if (isRestoring)
    return (
      <div className="grid min-h-screen place-items-center text-sm text-slate-600">
        Restoring your secure session…
      </div>
    );
  if (!user) return <NavigateToLogin from={location.pathname} />;
  if (!hasPermission(permission))
    return (
      <div className="mx-auto mt-24 max-w-lg text-center">
        <ShieldCheck className="mx-auto h-8 w-8 text-slate-400" />
        <h1 className="mt-3 text-lg font-semibold">Access restricted</h1>
        <p className="mt-1 text-sm text-slate-600">
          Your role does not include the permission required for this page.
        </p>
      </div>
    );
  return <Outlet />;
}

function NavigateToLogin({ from }: { from: string }) {
  const navigate = useNavigate();
  useEffect(() => {
    void navigate("/login", { replace: true, state: { from } });
  }, [from, navigate]);
  return null;
}
