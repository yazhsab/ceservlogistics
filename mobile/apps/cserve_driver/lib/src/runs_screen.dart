import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter/material.dart';

import 'stop_screen.dart';

class RunsScreen extends StatefulWidget {
  const RunsScreen({
    required this.api,
    required this.sessionStore,
    required this.profile,
    required this.onSignOut,
    super.key,
  });

  final CServeApiClient api;
  final SessionStore sessionStore;
  final UserProfile profile;
  final Future<void> Function() onSignOut;

  @override
  State<RunsScreen> createState() => _RunsScreenState();
}

class _RunsScreenState extends State<RunsScreen> {
  late Future<List<DeliveryRunSummary>> _runs;

  @override
  void initState() {
    super.initState();
    _runs = widget.api.listMyDeliveryRuns();
  }

  Future<void> _refresh() async {
    final future = widget.api.listMyDeliveryRuns();
    setState(() => _runs = future);
    await future;
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text('My runs', style: TextStyle(fontWeight: FontWeight.w800)),
          Text(
            'CServe Driver',
            style: TextStyle(fontSize: 12, fontWeight: FontWeight.w400),
          ),
        ],
      ),
      actions: <Widget>[
        PopupMenuButton<String>(
          tooltip: 'Account menu',
          onSelected: (value) {
            if (value == 'signout') widget.onSignOut();
          },
          itemBuilder: (context) => <PopupMenuEntry<String>>[
            PopupMenuItem<String>(
              enabled: false,
              child: Text(
                widget.profile.fullName,
                style: const TextStyle(fontWeight: FontWeight.w700),
              ),
            ),
            const PopupMenuDivider(),
            const PopupMenuItem<String>(
              value: 'signout',
              child: ListTile(
                contentPadding: EdgeInsets.zero,
                leading: Icon(Icons.logout),
                title: Text('Sign out'),
              ),
            ),
          ],
        ),
      ],
    ),
    body: FutureBuilder<List<DeliveryRunSummary>>(
      future: _runs,
      builder: (context, snapshot) {
        if (snapshot.connectionState == ConnectionState.waiting) {
          return const Center(child: CircularProgressIndicator());
        }
        if (snapshot.hasError) {
          return _ErrorState(error: snapshot.error, onRetry: _refresh);
        }
        final runs = snapshot.data ?? const <DeliveryRunSummary>[];
        if (runs.isEmpty) {
          return RefreshIndicator(
            onRefresh: _refresh,
            child: ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: const <Widget>[
                SizedBox(height: 140),
                Icon(Icons.route_outlined, size: 48, color: Colors.black38),
                SizedBox(height: 16),
                Text(
                  'No assigned runs',
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
                ),
                SizedBox(height: 6),
                Text(
                  'Pull down to check again.',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Colors.black54),
                ),
              ],
            ),
          );
        }
        return RefreshIndicator(
          onRefresh: _refresh,
          child: ListView.separated(
            padding: const EdgeInsets.all(16),
            itemCount: runs.length,
            separatorBuilder: (_, _) => const SizedBox(height: 12),
            itemBuilder: (context, index) => _RunCard(
              run: runs[index],
              onTap: () async {
                await Navigator.of(context).push<void>(
                  MaterialPageRoute<void>(
                    builder: (_) => RunDetailScreen(
                      api: widget.api,
                      sessionStore: widget.sessionStore,
                      profile: widget.profile,
                      runId: runs[index].id,
                    ),
                  ),
                );
                await _refresh();
              },
            ),
          ),
        );
      },
    ),
  );
}

class _RunCard extends StatelessWidget {
  const _RunCard({required this.run, required this.onTap});

  final DeliveryRunSummary run;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final progress = run.plannedStops == 0
        ? 0.0
        : (run.completedStops / run.plannedStops).clamp(0, 1).toDouble();
    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(
                          run.runCode,
                          style: const TextStyle(
                            fontSize: 18,
                            fontWeight: FontWeight.w800,
                          ),
                        ),
                        Text(
                          run.runDate,
                          style: const TextStyle(color: Colors.black54),
                        ),
                      ],
                    ),
                  ),
                  StatusPill(status: run.status),
                  const SizedBox(width: 4),
                  const Icon(Icons.chevron_right),
                ],
              ),
              const SizedBox(height: 18),
              Row(
                children: <Widget>[
                  Expanded(
                    child: _Metric(
                      value: '${run.completedStops}/${run.plannedStops}',
                      label: 'Stops',
                    ),
                  ),
                  Expanded(
                    child: _Metric(
                      value: '${run.deliveredCount}',
                      label: 'Delivered',
                    ),
                  ),
                  Expanded(
                    child: _Metric(
                      value: '${run.failedCount}',
                      label: 'Exceptions',
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              LinearProgressIndicator(
                value: progress,
                minHeight: 7,
                borderRadius: BorderRadius.circular(6),
              ),
              if (run.codExpectedMinor > 0) ...<Widget>[
                const SizedBox(height: 12),
                Text(
                  'COD ${money(run.codCollectedMinor, run.currency)} collected of '
                  '${money(run.codExpectedMinor, run.currency)}',
                  style: const TextStyle(fontSize: 13, color: Colors.black54),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class RunDetailScreen extends StatefulWidget {
  const RunDetailScreen({
    required this.api,
    required this.sessionStore,
    required this.profile,
    required this.runId,
    super.key,
  });

  final CServeApiClient api;
  final SessionStore sessionStore;
  final UserProfile profile;
  final String runId;

  @override
  State<RunDetailScreen> createState() => _RunDetailScreenState();
}

class _RunDetailScreenState extends State<RunDetailScreen> {
  late Future<DeliveryRun> _run;

  @override
  void initState() {
    super.initState();
    _run = widget.api.getDeliveryRun(widget.runId);
  }

  Future<void> _refresh() async {
    final future = widget.api.getDeliveryRun(widget.runId);
    setState(() => _run = future);
    await future;
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<DeliveryRun>(
    future: _run,
    builder: (context, snapshot) {
      final title = snapshot.data?.runCode ?? 'Delivery run';
      return Scaffold(
        appBar: AppBar(title: Text(title)),
        body: snapshot.connectionState == ConnectionState.waiting
            ? const Center(child: CircularProgressIndicator())
            : snapshot.hasError
            ? _ErrorState(error: snapshot.error, onRetry: _refresh)
            : _body(snapshot.data!),
      );
    },
  );

  Widget _body(DeliveryRun run) => RefreshIndicator(
    onRefresh: _refresh,
    child: ListView(
      padding: const EdgeInsets.all(16),
      children: <Widget>[
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Row(
                  children: <Widget>[
                    StatusPill(status: run.status),
                    const Spacer(),
                    Text('${run.completedStops}/${run.plannedStops} complete'),
                  ],
                ),
                const SizedBox(height: 12),
                LinearProgressIndicator(
                  value: run.plannedStops == 0
                      ? 0
                      : (run.completedStops / run.plannedStops)
                            .clamp(0, 1)
                            .toDouble(),
                  minHeight: 8,
                  borderRadius: BorderRadius.circular(6),
                ),
                if (run.codExpectedMinor > 0) ...<Widget>[
                  const SizedBox(height: 12),
                  Text(
                    'COD to collect: ${money(run.codExpectedMinor - run.codCollectedMinor, run.currency)}',
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                ],
              ],
            ),
          ),
        ),
        const SizedBox(height: 20),
        const Text(
          'Stops',
          style: TextStyle(fontSize: 20, fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 10),
        ...run.stops.map(
          (stop) => Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: Card(
              child: InkWell(
                borderRadius: BorderRadius.circular(12),
                onTap: () async {
                  await Navigator.of(context).push<void>(
                    MaterialPageRoute<void>(
                      builder: (_) => StopScreen(
                        api: widget.api,
                        sessionStore: widget.sessionStore,
                        profile: widget.profile,
                        runId: run.id,
                        stop: stop,
                      ),
                    ),
                  );
                  await _refresh();
                },
                child: Padding(
                  padding: const EdgeInsets.all(14),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      CircleAvatar(
                        backgroundColor: CServeColors.surfaceMuted,
                        foregroundColor: CServeColors.primaryDark,
                        child: Text('${stop.stopSequence}'),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: <Widget>[
                            Text(
                              stop.recipientName,
                              style: const TextStyle(
                                fontWeight: FontWeight.w800,
                                fontSize: 16,
                              ),
                            ),
                            const SizedBox(height: 3),
                            Text(
                              stop.address,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                            const SizedBox(height: 8),
                            Wrap(
                              spacing: 8,
                              runSpacing: 6,
                              children: <Widget>[
                                StatusPill(status: stop.status),
                                _SmallPill(
                                  label:
                                      '${stop.pieceCount} ${stop.pieceCount == 1 ? 'piece' : 'pieces'}',
                                  icon: Icons.inventory_2_outlined,
                                ),
                                if (stop.codAmountMinor > 0)
                                  _SmallPill(
                                    label: money(
                                      stop.codAmountMinor,
                                      stop.currency,
                                    ),
                                    icon: Icons.payments_outlined,
                                  ),
                              ],
                            ),
                          ],
                        ),
                      ),
                      const Icon(Icons.chevron_right),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    ),
  );
}

class _Metric extends StatelessWidget {
  const _Metric({required this.value, required this.label});

  final String value;
  final String label;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: <Widget>[
      Text(
        value,
        style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w800),
      ),
      Text(label, style: const TextStyle(fontSize: 12, color: Colors.black54)),
    ],
  );
}

class StatusPill extends StatelessWidget {
  const StatusPill({required this.status, super.key});

  final String status;

  @override
  Widget build(BuildContext context) {
    final color = switch (status) {
      'DELIVERED' || 'COMPLETED' || 'CLOSED' => CServeColors.success,
      'FAILED' || 'CANCELLED' => CServeColors.danger,
      'DISPATCHED' ||
      'IN_PROGRESS' ||
      'OUT_FOR_DELIVERY' => CServeColors.primary,
      _ => CServeColors.warning,
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(99),
      ),
      child: Text(
        status.replaceAll('_', ' '),
        style: TextStyle(
          color: color,
          fontWeight: FontWeight.w800,
          fontSize: 11,
        ),
      ),
    );
  }
}

class _SmallPill extends StatelessWidget {
  const _SmallPill({required this.label, required this.icon});

  final String label;
  final IconData icon;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
    decoration: BoxDecoration(
      color: CServeColors.surfaceMuted,
      borderRadius: BorderRadius.circular(99),
    ),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Icon(icon, size: 14),
        const SizedBox(width: 4),
        Text(
          label,
          style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.error, required this.onRetry});

  final Object? error;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          const Icon(Icons.cloud_off_outlined, size: 44),
          const SizedBox(height: 12),
          Text(
            error is ApiFailure
                ? (error! as ApiFailure).message
                : 'Could not load delivery runs.',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 16),
          FilledButton.tonal(
            onPressed: onRetry,
            child: const Text('Try again'),
          ),
        ],
      ),
    ),
  );
}

String money(int minor, String currency) {
  final absolute = minor.abs();
  final major = absolute ~/ 100;
  final cents = (absolute % 100).toString().padLeft(2, '0');
  return '${minor < 0 ? '-' : ''}${currency.isEmpty ? '' : '$currency '}$major.$cents';
}
