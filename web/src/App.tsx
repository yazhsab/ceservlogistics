import { lazy, Suspense, type ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useAuth } from "./auth/AuthProvider";
import { signedInHome } from "./auth/navigation";
import { ErrorState, LoadingState, PermissionDenied } from "./components/ui";
import { AppShell } from "./layout/AppShell";
import {
  ChangePasswordPage,
  ForgotPasswordPage,
  LoginPage,
  ResetPasswordPage,
} from "./pages/AuthPages";

const ProfilePage = lazy(() => import("./pages/ProfilePage"));
const OrganizationPage = lazy(() => import("./pages/OrganizationPage"));
const FranchiseCollectionsPage = lazy(() => import("./pages/CollectionPages"));
const UsersPage = lazy(() =>
  import("./pages/AdminPages").then((module) => ({
    default: module.UsersPage,
  })),
);
const UserDetailPage = lazy(() =>
  import("./pages/AdminPages").then((module) => ({
    default: module.UserDetailPage,
  })),
);
const RolesPage = lazy(() =>
  import("./pages/AdminPages").then((module) => ({
    default: module.RolesPage,
  })),
);
const OperatingUnitsPage = lazy(() =>
  import("./pages/NetworkPages").then((module) => ({
    default: module.OperatingUnitsPage,
  })),
);
const OperatingUnitDetailPage = lazy(() =>
  import("./pages/NetworkPages").then((module) => ({
    default: module.OperatingUnitDetailPage,
  })),
);
const FranchisesPage = lazy(() =>
  import("./pages/NetworkPages").then((module) => ({
    default: module.FranchisesPage,
  })),
);
const PincodesPage = lazy(() =>
  import("./pages/GeographyPages").then((module) => ({
    default: module.PincodesPage,
  })),
);
const ZonesPage = lazy(() =>
  import("./pages/GeographyPages").then((module) => ({
    default: module.ZonesPage,
  })),
);
const GeographyImportsPage = lazy(() =>
  import("./pages/GeographyPages").then((module) => ({
    default: module.GeographyImportsPage,
  })),
);
const ServiceabilityPage = lazy(() =>
  import("./pages/RoutingPages").then((module) => ({
    default: module.ServiceabilityPage,
  })),
);
const RoutesPage = lazy(() =>
  import("./pages/RoutingPages").then((module) => ({
    default: module.RoutesPage,
  })),
);
const RoutingRulesPage = lazy(() =>
  import("./pages/RoutingPages").then((module) => ({
    default: module.RoutingRulesPage,
  })),
);
const OverridesPage = lazy(() =>
  import("./pages/RoutingPages").then((module) => ({
    default: module.OverridesPage,
  })),
);
const ProductsPage = lazy(() =>
  import("./pages/ProductPages").then((module) => ({
    default: module.ProductsPage,
  })),
);
const ProductDetailPage = lazy(() =>
  import("./pages/ProductPages").then((module) => ({
    default: module.ProductDetailPage,
  })),
);
const PricingSimulatorPage = lazy(() =>
  import("./pages/PricingPages").then((module) => ({
    default: module.PricingSimulatorPage,
  })),
);
const RateCardsPage = lazy(() =>
  import("./pages/PricingPages").then((module) => ({
    default: module.RateCardsPage,
  })),
);
const PricingMastersPage = lazy(() => import("./pages/PricingMasterPages"));
const RateCardDetailPage = lazy(() =>
  import("./pages/PricingPages").then((module) => ({
    default: module.RateCardDetailPage,
  })),
);
const RateCardVersionPage = lazy(() =>
  import("./pages/PricingPages").then((module) => ({
    default: module.RateCardVersionPage,
  })),
);
const CustomersPage = lazy(() =>
  import("./pages/CustomerPages").then((module) => ({
    default: module.CustomersPage,
  })),
);
const CustomerDetailPage = lazy(() =>
  import("./pages/CustomerPages").then((module) => ({
    default: module.CustomerDetailPage,
  })),
);
const BookingPage = lazy(() => import("./pages/BookingPage"));
const ShipmentsPage = lazy(() =>
  import("./pages/ShipmentPages").then((module) => ({
    default: module.ShipmentsPage,
  })),
);
const ShipmentDetailPage = lazy(() =>
  import("./pages/ShipmentPages").then((module) => ({
    default: module.ShipmentDetailPage,
  })),
);
const PublicTrackingPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.PublicTrackingPage,
  })),
);
const PickupDashboardPage = lazy(() =>
  import("./pages/PickupScannerPages").then((module) => ({
    default: module.PickupDashboardPage,
  })),
);
const PickupDetailPage = lazy(() =>
  import("./pages/PickupScannerPages").then((module) => ({
    default: module.PickupDetailPage,
  })),
);
const PickupRunsPage = lazy(() =>
  import("./pages/PickupScannerPages").then((module) => ({
    default: module.PickupRunsPage,
  })),
);
const PickupAgentPage = lazy(() =>
  import("./pages/PickupScannerPages").then((module) => ({
    default: module.PickupAgentPage,
  })),
);
const ScannerConsolePage = lazy(() =>
  import("./pages/PickupScannerPages").then((module) => ({
    default: module.ScannerConsolePage,
  })),
);
const BagsPage = lazy(() =>
  import("./pages/BagManifestPages").then((module) => ({
    default: module.BagsPage,
  })),
);
const BagDetailPage = lazy(() =>
  import("./pages/BagManifestPages").then((module) => ({
    default: module.BagDetailPage,
  })),
);
const ReceiveBagPage = lazy(() =>
  import("./pages/BagManifestPages").then((module) => ({
    default: module.ReceiveBagPage,
  })),
);
const ManifestsPage = lazy(() =>
  import("./pages/BagManifestPages").then((module) => ({
    default: module.ManifestsPage,
  })),
);
const ManifestDetailPage = lazy(() =>
  import("./pages/BagManifestPages").then((module) => ({
    default: module.ManifestDetailPage,
  })),
);
const ReceiveManifestPage = lazy(() =>
  import("./pages/BagManifestPages").then((module) => ({
    default: module.ReceiveManifestPage,
  })),
);
const TripsPage = lazy(() =>
  import("./pages/TripPages").then((module) => ({ default: module.TripsPage })),
);
const TripDetailPage = lazy(() =>
  import("./pages/TripPages").then((module) => ({
    default: module.TripDetailPage,
  })),
);
const FleetPage = lazy(() =>
  import("./pages/TripPages").then((module) => ({ default: module.FleetPage })),
);
const HubConsolePage = lazy(() =>
  import("./pages/HubPages").then((module) => ({
    default: module.HubConsolePage,
  })),
);
const ReconciliationDetailPage = lazy(() =>
  import("./pages/HubPages").then((module) => ({
    default: module.ReconciliationDetailPage,
  })),
);
const DestinationQueuePage = lazy(() =>
  import("./pages/DeliveryPages").then((module) => ({
    default: module.DestinationQueuePage,
  })),
);
const DeliveryDispatcherPage = lazy(() =>
  import("./pages/DeliveryPages").then((module) => ({
    default: module.DeliveryDispatcherPage,
  })),
);
const DeliveryRunDetailPage = lazy(() =>
  import("./pages/DeliveryPages").then((module) => ({
    default: module.DeliveryRunDetailPage,
  })),
);
const DeliveryMyRunPage = lazy(() =>
  import("./pages/DeliveryPages").then((module) => ({
    default: module.DeliveryMyRunPage,
  })),
);
const DeliveryStopPage = lazy(() =>
  import("./pages/DeliveryPages").then((module) => ({
    default: module.DeliveryStopPage,
  })),
);
const NDRWorkbenchPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.NDRWorkbenchPage,
  })),
);
const NDRDetailPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.NDRDetailPage,
  })),
);
const RTOPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.RTOPage,
  })),
);
const RTODetailPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.RTODetailPage,
  })),
);
const SubmitPODPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.SubmitPODPage,
  })),
);
const PODViewerPage = lazy(() =>
  import("./pages/ExceptionTrackingPages").then((module) => ({
    default: module.PODViewerPage,
  })),
);
const CommissionRulesPage = lazy(() =>
  import("./pages/CommissionPages").then((module) => ({
    default: module.CommissionRulesPage,
  })),
);
const CommissionRuleDetailPage = lazy(() =>
  import("./pages/CommissionPages").then((module) => ({
    default: module.CommissionRuleDetailPage,
  })),
);
const CommissionSimulatorPage = lazy(() =>
  import("./pages/CommissionPages").then((module) => ({
    default: module.CommissionSimulatorPage,
  })),
);
const CommissionEntriesPage = lazy(() =>
  import("./pages/CommissionPages").then((module) => ({
    default: module.CommissionEntriesPage,
  })),
);
const CommissionEntryDetailPage = lazy(() =>
  import("./pages/CommissionPages").then((module) => ({
    default: module.CommissionEntryDetailPage,
  })),
);
const LedgerAccountsPage = lazy(() =>
  import("./pages/LedgerPages").then((module) => ({
    default: module.LedgerAccountsPage,
  })),
);
const AccountStatementPage = lazy(() =>
  import("./pages/LedgerPages").then((module) => ({
    default: module.AccountStatementPage,
  })),
);
const JournalTransactionsPage = lazy(() =>
  import("./pages/LedgerPages").then((module) => ({
    default: module.JournalTransactionsPage,
  })),
);
const JournalDetailPage = lazy(() =>
  import("./pages/LedgerPages").then((module) => ({
    default: module.JournalDetailPage,
  })),
);
const TrialBalancePage = lazy(() =>
  import("./pages/LedgerPages").then((module) => ({
    default: module.TrialBalancePage,
  })),
);
const CODControlCenterPage = lazy(() =>
  import("./pages/CODPages").then((module) => ({
    default: module.CODControlCenterPage,
  })),
);
const CODObligationDetailPage = lazy(() =>
  import("./pages/CODPages").then((module) => ({
    default: module.CODObligationDetailPage,
  })),
);
const SettlementsPage = lazy(() =>
  import("./pages/SettlementPages").then((module) => ({
    default: module.SettlementsPage,
  })),
);
const SettlementDetailPage = lazy(() =>
  import("./pages/SettlementPages").then((module) => ({
    default: module.SettlementDetailPage,
  })),
);
const InvoicesPage = lazy(() =>
  import("./pages/BillingPages").then((module) => ({
    default: module.InvoicesPage,
  })),
);
const InvoiceDetailPage = lazy(() =>
  import("./pages/BillingPages").then((module) => ({
    default: module.InvoiceDetailPage,
  })),
);
const APIKeysPage = lazy(() =>
  import("./pages/IntegrationPages").then((module) => ({
    default: module.APIKeysPage,
  })),
);
const WebhookEndpointsPage = lazy(() =>
  import("./pages/IntegrationPages").then((module) => ({
    default: module.WebhookEndpointsPage,
  })),
);
const WebhookDeliveriesPage = lazy(() =>
  import("./pages/IntegrationPages").then((module) => ({
    default: module.WebhookDeliveriesPage,
  })),
);
const WebhookDeliveryDetailPage = lazy(() =>
  import("./pages/IntegrationPages").then((module) => ({
    default: module.WebhookDeliveryDetailPage,
  })),
);
const NotificationTemplatesPage = lazy(() =>
  import("./pages/NotificationPages").then((module) => ({
    default: module.NotificationTemplatesPage,
  })),
);
const NotificationTriggersPage = lazy(() =>
  import("./pages/NotificationPages").then((module) => ({
    default: module.NotificationTriggersPage,
  })),
);
const NotificationChannelsPage = lazy(() =>
  import("./pages/NotificationPages").then((module) => ({
    default: module.NotificationChannelsPage,
  })),
);
const NotificationDeliveriesPage = lazy(() =>
  import("./pages/NotificationPages").then((module) => ({
    default: module.NotificationDeliveriesPage,
  })),
);
const NotificationDeliveryDetailPage = lazy(() =>
  import("./pages/NotificationPages").then((module) => ({
    default: module.NotificationDeliveryDetailPage,
  })),
);
const CommandCentrePage = lazy(() =>
  import("./pages/CommandReportPages").then((module) => ({
    default: module.CommandCentrePage,
  })),
);
const ReportCenterPage = lazy(() =>
  import("./pages/CommandReportPages").then((module) => ({
    default: module.ReportCenterPage,
  })),
);
const CustomerPortalLayout = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.CustomerPortalLayout,
  })),
);
const CustomerPortalDashboardPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.CustomerPortalDashboardPage,
  })),
);
const CustomerShipmentsPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.CustomerShipmentsPage,
  })),
);
const CustomerTrackingDetailPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.CustomerTrackingDetailPage,
  })),
);
const CustomerInvoicesPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.CustomerInvoicesPage,
  })),
);
const PortalProfilePage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.PortalProfilePage,
  })),
);
const FranchisePortalLayout = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.FranchisePortalLayout,
  })),
);
const FranchisePortalDashboardPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.FranchisePortalDashboardPage,
  })),
);
const FranchiseShipmentsPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.FranchiseShipmentsPage,
  })),
);
const FranchiseSettlementsPage = lazy(() =>
  import("./pages/PortalPages").then((module) => ({
    default: module.FranchiseSettlementsPage,
  })),
);

export default function App() {
  return (
    <Suspense fallback={<LoadingState label="Opening workspace" />}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/forgot-password" element={<ForgotPasswordPage />} />
        <Route path="/reset-password" element={<ResetPasswordPage />} />
        <Route path="/track" element={<PublicTrackingPage />} />
        <Route path="/track/:awb" element={<PublicTrackingPage />} />
        <Route
          path="/change-password"
          element={
            <SignedIn>
              <ChangePasswordPage />
            </SignedIn>
          }
        />
        <Route
          element={
            <SignedIn requirePasswordChanged>
              <CustomerPortalLayout />
            </SignedIn>
          }
        >
          <Route
            path="portal/customer"
            element={<CustomerPortalDashboardPage />}
          />
          <Route
            path="portal/customer/shipments"
            element={<CustomerShipmentsPage />}
          />
          <Route
            path="portal/customer/shipments/:shipmentId"
            element={<CustomerTrackingDetailPage />}
          />
          <Route
            path="portal/customer/invoices"
            element={<CustomerInvoicesPage />}
          />
          <Route
            path="portal/customer/profile"
            element={<PortalProfilePage audience="customer" />}
          />
        </Route>
        <Route
          element={
            <SignedIn requirePasswordChanged>
              <FranchisePortalLayout />
            </SignedIn>
          }
        >
          <Route
            path="portal/franchise"
            element={<FranchisePortalDashboardPage />}
          />
          <Route
            path="portal/franchise/shipments"
            element={<FranchiseShipmentsPage />}
          />
          <Route
            path="portal/franchise/settlements"
            element={<FranchiseSettlementsPage />}
          />
          <Route
            path="portal/franchise/profile"
            element={<PortalProfilePage audience="franchise" />}
          />
        </Route>
        <Route
          element={
            <SignedIn requirePasswordChanged staffWorkspace>
              <AppShell />
            </SignedIn>
          }
        >
          <Route index element={<SignedInHome />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route
            path="shipments"
            element={
              <Permission permission="shipment.read">
                <ShipmentsPage />
              </Permission>
            }
          />
          <Route
            path="shipments/new"
            element={
              <Permission permission="shipment.create">
                <BookingPage />
              </Permission>
            }
          />
          <Route
            path="shipments/:shipmentId"
            element={
              <Permission permission="shipment.read">
                <ShipmentDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/pickups"
            element={
              <Permission permission="pickup.read">
                <PickupDashboardPage />
              </Permission>
            }
          />
          <Route
            path="operations/pickups/runs"
            element={
              <Permission permission="pickup.read">
                <PickupRunsPage />
              </Permission>
            }
          />
          <Route
            path="operations/pickups/my-stops"
            element={
              <Permission permission="pickup.read">
                <PickupAgentPage />
              </Permission>
            }
          />
          <Route
            path="operations/pickups/:pickupId"
            element={
              <Permission permission="pickup.read">
                <PickupDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/scanner"
            element={
              <PermissionAny
                permissions={[
                  "scan.inbound",
                  "scan.outbound",
                  "scan.sort",
                  "scan.hold",
                  "scan.exception",
                  "scan.read",
                ]}
              >
                <ScannerConsolePage />
              </PermissionAny>
            }
          />
          <Route
            path="operations/bags"
            element={
              <Permission permission="bag.read">
                <BagsPage />
              </Permission>
            }
          />
          <Route
            path="operations/bags/receive"
            element={
              <Permission permission="bag.read">
                <ReceiveBagPage />
              </Permission>
            }
          />
          <Route
            path="operations/bags/:bagId"
            element={
              <Permission permission="bag.read">
                <BagDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/manifests"
            element={
              <Permission permission="manifest.read">
                <ManifestsPage />
              </Permission>
            }
          />
          <Route
            path="operations/manifests/receive"
            element={
              <Permission permission="manifest.read">
                <ReceiveManifestPage />
              </Permission>
            }
          />
          <Route
            path="operations/manifests/:manifestId"
            element={
              <Permission permission="manifest.read">
                <ManifestDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/trips"
            element={
              <Permission permission="trip.read">
                <TripsPage />
              </Permission>
            }
          />
          <Route
            path="operations/trips/:tripId"
            element={
              <Permission permission="trip.read">
                <TripDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/fleet"
            element={
              <PermissionAny
                permissions={["vehicle.read", "carrier.read", "driver.read"]}
              >
                <FleetPage />
              </PermissionAny>
            }
          />
          <Route
            path="operations/hub"
            element={
              <PermissionAny permissions={["hub.dashboard", "portal.console"]}>
                <HubConsolePage />
              </PermissionAny>
            }
          />
          <Route
            path="command-centre"
            element={
              <Permission permission="command.read">
                <CommandCentrePage />
              </Permission>
            }
          />
          <Route
            path="reports"
            element={
              <Permission permission="report.read">
                <ReportCenterPage />
              </Permission>
            }
          />
          <Route
            path="operations/hub/reconciliations/:reconciliationId"
            element={
              <Permission permission="reconciliation.manage">
                <ReconciliationDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/destination"
            element={
              <Permission permission="delivery.read">
                <DestinationQueuePage />
              </Permission>
            }
          />
          <Route
            path="operations/delivery"
            element={
              <Permission permission="delivery.read">
                <DeliveryDispatcherPage />
              </Permission>
            }
          />
          <Route
            path="operations/delivery/my-run"
            element={
              <Permission permission="delivery.read">
                <DeliveryMyRunPage />
              </Permission>
            }
          />
          <Route
            path="operations/delivery/runs/:runId"
            element={
              <Permission permission="delivery.read">
                <DeliveryRunDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/delivery/runs/:runId/stops/:awb"
            element={
              <Permission permission="delivery.read">
                <DeliveryStopPage />
              </Permission>
            }
          />
          <Route
            path="operations/ndr"
            element={
              <Permission permission="ndr.read">
                <NDRWorkbenchPage />
              </Permission>
            }
          />
          <Route
            path="operations/ndr/:caseId"
            element={
              <Permission permission="ndr.read">
                <NDRDetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/rto"
            element={
              <Permission permission="rto.read">
                <RTOPage />
              </Permission>
            }
          />
          <Route
            path="operations/rto/:caseId"
            element={
              <Permission permission="rto.read">
                <RTODetailPage />
              </Permission>
            }
          />
          <Route
            path="operations/pod/new"
            element={
              <Permission permission="pod.submit">
                <SubmitPODPage />
              </Permission>
            }
          />
          <Route
            path="operations/pod/:podId"
            element={
              <Permission permission="pod.read">
                <PODViewerPage />
              </Permission>
            }
          />
          <Route
            path="admin/users"
            element={
              <Permission permission="user.read">
                <UsersPage />
              </Permission>
            }
          />
          <Route
            path="finance/commission/rules"
            element={
              <Permission permission="commission.read">
                <CommissionRulesPage />
              </Permission>
            }
          />
          <Route
            path="finance/commission/rules/:ruleId"
            element={
              <Permission permission="commission.read">
                <CommissionRuleDetailPage />
              </Permission>
            }
          />
          <Route
            path="finance/collections"
            element={
              <Permission permission="collection.read">
                <FranchiseCollectionsPage />
              </Permission>
            }
          />
          <Route
            path="finance/commission/simulator"
            element={
              <Permission permission="commission.simulate">
                <CommissionSimulatorPage />
              </Permission>
            }
          />
          <Route
            path="finance/commission/entries"
            element={
              <Permission permission="commission.read">
                <CommissionEntriesPage />
              </Permission>
            }
          />
          <Route
            path="finance/commission/entries/:calculationId"
            element={
              <Permission permission="commission.read">
                <CommissionEntryDetailPage />
              </Permission>
            }
          />
          <Route
            path="finance/ledger/accounts"
            element={
              <Permission permission="ledger.read">
                <LedgerAccountsPage />
              </Permission>
            }
          />
          <Route
            path="finance/ledger/accounts/:accountId"
            element={
              <Permission permission="ledger.read">
                <AccountStatementPage />
              </Permission>
            }
          />
          <Route
            path="finance/ledger/journals"
            element={
              <Permission permission="ledger.read">
                <JournalTransactionsPage />
              </Permission>
            }
          />
          <Route
            path="finance/ledger/journals/:journalId"
            element={
              <Permission permission="ledger.read">
                <JournalDetailPage />
              </Permission>
            }
          />
          <Route
            path="finance/ledger/trial-balance"
            element={
              <Permission permission="ledger.read">
                <TrialBalancePage />
              </Permission>
            }
          />
          <Route
            path="finance/cod"
            element={
              <Permission permission="cod.read">
                <CODControlCenterPage />
              </Permission>
            }
          />
          <Route
            path="finance/cod/obligations/:obligationId"
            element={
              <Permission permission="cod.read">
                <CODObligationDetailPage />
              </Permission>
            }
          />
          <Route
            path="finance/settlements"
            element={
              <Permission permission="settlement.read">
                <SettlementsPage />
              </Permission>
            }
          />
          <Route
            path="finance/settlements/:settlementId"
            element={
              <Permission permission="settlement.read">
                <SettlementDetailPage />
              </Permission>
            }
          />
          <Route
            path="finance/billing/invoices"
            element={
              <Permission permission="invoice.read">
                <InvoicesPage />
              </Permission>
            }
          />
          <Route
            path="finance/billing/invoices/:invoiceId"
            element={
              <Permission permission="invoice.read">
                <InvoiceDetailPage />
              </Permission>
            }
          />
          <Route
            path="admin/users/:userId"
            element={
              <Permission permission="user.read">
                <UserDetailPage />
              </Permission>
            }
          />
          <Route
            path="admin/roles"
            element={
              <Permission permission="user.read">
                <RolesPage />
              </Permission>
            }
          />
          <Route
            path="admin/organization"
            element={
              <Permission permission="organization.read">
                <OrganizationPage />
              </Permission>
            }
          />
          <Route
            path="admin/integrations/api-keys"
            element={
              <Permission permission="apikey.read">
                <APIKeysPage />
              </Permission>
            }
          />
          <Route
            path="admin/notifications/templates"
            element={
              <Permission permission="notification.read">
                <NotificationTemplatesPage />
              </Permission>
            }
          />
          <Route
            path="admin/notifications/triggers"
            element={
              <Permission permission="notification.read">
                <NotificationTriggersPage />
              </Permission>
            }
          />
          <Route
            path="admin/notifications/channels"
            element={
              <Permission permission="notification.read">
                <NotificationChannelsPage />
              </Permission>
            }
          />
          <Route
            path="admin/notifications/deliveries"
            element={
              <Permission permission="notification.read">
                <NotificationDeliveriesPage />
              </Permission>
            }
          />
          <Route
            path="admin/notifications/deliveries/:notificationId"
            element={
              <Permission permission="notification.read">
                <NotificationDeliveryDetailPage />
              </Permission>
            }
          />
          <Route
            path="admin/integrations/webhooks"
            element={
              <Permission permission="webhook.read">
                <WebhookEndpointsPage />
              </Permission>
            }
          />
          <Route
            path="admin/integrations/webhooks/deliveries"
            element={
              <Permission permission="webhook.read">
                <WebhookDeliveriesPage />
              </Permission>
            }
          />
          <Route
            path="admin/integrations/webhooks/deliveries/:deliveryId"
            element={
              <Permission permission="webhook.read">
                <WebhookDeliveryDetailPage />
              </Permission>
            }
          />
          <Route
            path="network/hubs"
            element={
              <Permission permission="operating_unit.read">
                <OperatingUnitsPage kind="hubs" />
              </Permission>
            }
          />
          <Route
            path="network/branches"
            element={
              <Permission permission="operating_unit.read">
                <OperatingUnitsPage kind="branches" />
              </Permission>
            }
          />
          <Route
            path="network/units/:unitId"
            element={
              <Permission permission="operating_unit.read">
                <OperatingUnitDetailPage />
              </Permission>
            }
          />
          <Route
            path="network/franchises"
            element={
              <Permission permission="franchise.read">
                <FranchisesPage />
              </Permission>
            }
          />
          <Route
            path="geography/pincodes"
            element={
              <Permission permission="pincode.read">
                <PincodesPage />
              </Permission>
            }
          />
          <Route
            path="geography/zones"
            element={
              <Permission permission="zone.read">
                <ZonesPage />
              </Permission>
            }
          />
          <Route
            path="geography/imports"
            element={
              <Permission permission="zone.read">
                <GeographyImportsPage />
              </Permission>
            }
          />
          <Route
            path="routing/tester"
            element={
              <Permission permission="serviceability.check">
                <ServiceabilityPage />
              </Permission>
            }
          />
          <Route
            path="routing/rules"
            element={
              <Permission permission="service_area.read">
                <RoutingRulesPage />
              </Permission>
            }
          />
          <Route
            path="routing/routes"
            element={
              <Permission permission="route.read">
                <RoutesPage />
              </Permission>
            }
          />
          <Route
            path="routing/overrides"
            element={
              <Permission permission="route.manage">
                <OverridesPage />
              </Permission>
            }
          />
          <Route
            path="products"
            element={
              <Permission permission="courier_service.read">
                <ProductsPage />
              </Permission>
            }
          />
          <Route
            path="products/:serviceId"
            element={
              <Permission permission="courier_service.read">
                <ProductDetailPage />
              </Permission>
            }
          />
          <Route
            path="pricing/simulator"
            element={
              <Permission permission="pricing.quote">
                <PricingSimulatorPage />
              </Permission>
            }
          />
          <Route
            path="pricing/masters"
            element={
              <Permission permission="rate_card.read">
                <PricingMastersPage />
              </Permission>
            }
          />
          <Route
            path="pricing/rate-cards"
            element={
              <Permission permission="rate_card.read">
                <RateCardsPage />
              </Permission>
            }
          />
          <Route
            path="pricing/rate-cards/:cardId"
            element={
              <Permission permission="rate_card.read">
                <RateCardDetailPage />
              </Permission>
            }
          />
          <Route
            path="pricing/versions/:versionId"
            element={
              <Permission permission="rate_card.read">
                <RateCardVersionPage />
              </Permission>
            }
          />
          <Route
            path="customers"
            element={
              <Permission permission="customer.read">
                <CustomersPage />
              </Permission>
            }
          />
          <Route
            path="customers/:customerId"
            element={
              <Permission permission="customer.read">
                <CustomerDetailPage />
              </Permission>
            }
          />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </Suspense>
  );
}

function SignedIn({
  children,
  requirePasswordChanged = false,
  staffWorkspace = false,
}: {
  children: ReactNode;
  requirePasswordChanged?: boolean;
  staffWorkspace?: boolean;
}) {
  const { user, isRestoring } = useAuth();
  const location = useLocation();
  if (isRestoring)
    return (
      <div className="grid min-h-screen place-items-center">
        <LoadingState label="Restoring secure session" />
      </div>
    );
  if (!user)
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  if (requirePasswordChanged && user.mustChangePassword)
    return <Navigate to="/change-password" replace />;
  if (staffWorkspace && user.portal?.isCustomerUser)
    return <Navigate to="/portal/customer" replace />;
  return children;
}
function SignedInHome() {
  const { user } = useAuth();
  return <Navigate to={signedInHome(user)} replace />;
}
function Permission({
  permission,
  children,
}: {
  permission: string;
  children: ReactNode;
}) {
  const { hasPermission } = useAuth();
  return hasPermission(permission) ? children : <PermissionDenied />;
}
function PermissionAny({
  permissions,
  children,
}: {
  permissions: string[];
  children: ReactNode;
}) {
  const { hasPermission } = useAuth();
  return permissions.some((permission) => hasPermission(permission)) ? (
    children
  ) : (
    <PermissionDenied />
  );
}
function NotFound() {
  return (
    <div className="mx-auto max-w-xl">
      <ErrorState
        title="Page not found"
        error={
          new Error(
            "This page does not exist or is not available for your current release.",
          )
        }
      />
    </div>
  );
}
