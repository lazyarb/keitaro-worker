<?php

/**
 * LazyArb durable postback worker for Keitaro 10 and 11.
 *
 * SPDX-License-Identifier: MIT
 */

$queueRoot = trim((string) getenv('LAZYARB_KEITARO_QUEUE_ROOT'));
$logFile = trim((string) getenv('LAZYARB_KEITARO_LOG_FILE'));
if ($queueRoot === '' || $logFile === '' || strpos($queueRoot, "\0") !== false || strpos($logFile, "\0") !== false) {
    fwrite(STDERR, "LazyArb worker paths are not configured.\n");
    exit(1);
}
define('QUEUE_ROOT', rtrim($queueRoot, '/'));
define('LOG_FILE', $logFile);
const WORKER_VERSION = '1.0.0';
const BATCH_SIZE = 25;
const RETRY_BATCH_SIZE = 5;
const MAX_ATTEMPTS = 8;

function workerLog(string $level, string $message, array $context = []): void
{
    $payload = [
        'time' => gmdate('c'),
        'level' => $level,
        'message' => $message,
    ];
    if ($context !== []) {
        $payload['context'] = $context;
    }
    $line = json_encode($payload, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
    if (!is_string($line) || file_put_contents(LOG_FILE, $line . PHP_EOL, FILE_APPEND | LOCK_EX) === false) {
        fwrite(STDERR, '[LazyArb worker] ' . $message . PHP_EOL);
    }
}

function milliseconds(float $seconds): float
{
    return round(max(0.0, $seconds) * 1000, 3);
}

function curlProfile($handle): array
{
    $dnsAt = max(0.0, (float) curl_getinfo($handle, CURLINFO_NAMELOOKUP_TIME));
    $connectAt = max($dnsAt, (float) curl_getinfo($handle, CURLINFO_CONNECT_TIME));
    $tlsAt = max(0.0, (float) curl_getinfo($handle, CURLINFO_APPCONNECT_TIME));
    $preTransferAt = max($connectAt, $tlsAt, (float) curl_getinfo($handle, CURLINFO_PRETRANSFER_TIME));
    $firstByteAt = max($preTransferAt, (float) curl_getinfo($handle, CURLINFO_STARTTRANSFER_TIME));
    $totalAt = max($firstByteAt, (float) curl_getinfo($handle, CURLINFO_TOTAL_TIME));

    return [
        'dns_ms' => milliseconds($dnsAt),
        'connect_ms' => milliseconds($connectAt - $dnsAt),
        'tls_ms' => milliseconds($tlsAt > 0.0 ? $tlsAt - $connectAt : 0.0),
        'server_ms' => milliseconds($firstByteAt - $preTransferAt),
        'download_ms' => milliseconds($totalAt - $firstByteAt),
        'curl_total_ms' => milliseconds($totalAt),
    ];
}

function queuedFiles(string $directory, int $limit): array
{
    $files = [];
    foreach (new DirectoryIterator($directory) as $entry) {
        if ($entry->isFile() && substr($entry->getFilename(), -5) === '.json') {
            $files[] = $entry->getPathname();
            if (count($files) >= $limit) {
                break;
            }
        }
    }
    sort($files, SORT_STRING);
    return $files;
}

function dueRetryFiles(int $limit, int $scanLimit): array
{
    $due = [];
    $now = time();
    foreach (queuedFiles(QUEUE_ROOT . '/retry', $scanLimit) as $path) {
        $event = json_decode((string) file_get_contents($path), true);
        if (!is_array($event) || (int) ($event['next_attempt_at'] ?? 0) <= $now) {
            $due[] = $path;
            if (count($due) >= $limit) {
                break;
            }
        }
    }
    return $due;
}

function moveEvent(string $source, string $directory): string
{
    $target = $directory . '/' . basename($source);
    if (!rename($source, $target)) {
        throw new RuntimeException('could not move queue item');
    }
    return $target;
}

function writeEventAtomically(string $path, array $event): void
{
    $payload = json_encode($event, JSON_UNESCAPED_SLASHES);
    if (!is_string($payload)) {
        throw new RuntimeException('could not encode queue item');
    }
    $temporaryPath = QUEUE_ROOT . '/tmp/worker-' . bin2hex(random_bytes(12)) . '.writing';
    $handle = fopen($temporaryPath, 'xb');
    if ($handle === false) {
        throw new RuntimeException('could not open temporary queue item');
    }
    $offset = 0;
    $length = strlen($payload);
    try {
        while ($offset < $length) {
            $written = fwrite($handle, substr($payload, $offset));
            if ($written === false || $written === 0) {
                throw new RuntimeException('could not write queue item');
            }
            $offset += $written;
        }
        fflush($handle);
        if (function_exists('fsync')) {
            fsync($handle);
        }
    } finally {
        fclose($handle);
    }
    chmod($temporaryPath, 0660);
    if (!rename($temporaryPath, $path)) {
        @unlink($temporaryPath);
        throw new RuntimeException('could not publish queue item state');
    }
}

function retryEvent(string $path, array $event): void
{
    $event['attempts'] = (int) ($event['attempts'] ?? 0) + 1;
    if ($event['attempts'] >= MAX_ATTEMPTS) {
        writeEventAtomically($path, $event);
        moveEvent($path, QUEUE_ROOT . '/failed');
        workerLog('error', 'event_moved_to_dead_letter', ['attempts' => $event['attempts']]);
        return;
    }
    $event['next_attempt_at'] = time() + min(300, 2 ** $event['attempts']);
    writeEventAtomically($path, $event);
    moveEvent($path, QUEUE_ROOT . '/retry');
    workerLog('warning', 'event_retry_scheduled', ['attempts' => $event['attempts']]);
}

function deliver(array $items): array
{
    $multi = curl_multi_init();
    $handles = [];
    foreach ($items as $item) {
        $endpoint = (string) ($item['event']['endpoint'] ?? '');
        $query = http_build_query([
            'code' => $item['event']['code'],
            'ad_id' => $item['event']['ad_id'],
            'subid' => $item['event']['subid'],
        ], '', '&', PHP_QUERY_RFC3986);
        $handle = curl_init($endpoint . '?' . $query);
        if ($handle === false) {
            $handles[] = ['handle' => null, 'item' => $item, 'error' => 'curl_init_failed'];
            continue;
        }
        curl_setopt_array($handle, [
            CURLOPT_HTTPGET => true,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_FOLLOWLOCATION => false,
            CURLOPT_CONNECTTIMEOUT_MS => 1000,
            CURLOPT_TIMEOUT_MS => 5000,
            CURLOPT_NOSIGNAL => true,
            CURLOPT_SSL_VERIFYPEER => true,
            CURLOPT_SSL_VERIFYHOST => 2,
            CURLOPT_USERAGENT => 'LazyArb-Keitaro-Worker/1.0',
        ]);
        $handles[] = ['handle' => $handle, 'item' => $item, 'error' => ''];
        curl_multi_add_handle($multi, $handle);
    }
    do {
        $status = curl_multi_exec($multi, $running);
        if ($running > 0) {
            curl_multi_select($multi, 1.0);
        }
    } while ($running > 0 && $status === CURLM_OK);

    $results = [];
    foreach ($handles as $entry) {
        $handle = $entry['handle'];
        if ($handle === null) {
            $results[] = ['item' => $entry['item'], 'ok' => false, 'http_code' => 0, 'curl_errno' => 0, 'error' => $entry['error'], 'profile' => []];
            continue;
        }
        $httpCode = (int) curl_getinfo($handle, CURLINFO_HTTP_CODE);
        $curlErrno = curl_errno($handle);
        $profile = curlProfile($handle);
        $results[] = [
            'item' => $entry['item'],
            'ok' => $curlErrno === 0 && $httpCode >= 200 && $httpCode < 300,
            'http_code' => $httpCode,
            'curl_errno' => $curlErrno,
            'error' => '',
            'profile' => $profile,
        ];
        curl_multi_remove_handle($multi, $handle);
        curl_close($handle);
    }
    curl_multi_close($multi);
    return $results;
}

function recoverQueueFiles(string $sourceDirectory, string $targetDirectory): int
{
    $recovered = 0;
    foreach (queuedFiles($sourceDirectory, PHP_INT_MAX) as $path) {
        moveEvent($path, $targetDirectory);
        ++$recovered;
    }
    return $recovered;
}

function recoverPublisherFiles(): int
{
    $recovered = 0;
    $now = time();
    foreach (new DirectoryIterator(QUEUE_ROOT . '/tmp') as $entry) {
        if (!$entry->isFile() || $now - $entry->getMTime() < 5) {
            continue;
        }
        $name = $entry->getFilename();
        $path = $entry->getPathname();
        if (strpos($name, 'worker-') === 0 && substr($name, -8) === '.writing') {
            // The original processing item remains authoritative until replacement.
            @unlink($path);
            continue;
        }
        if (strpos($name, 'enqueue-') !== 0 || substr($name, -13) !== '.json.writing') {
            continue;
        }
        $event = json_decode((string) file_get_contents($path), true);
        $targetName = substr($name, 8, -8);
        if (validEvent($event)) {
            if (!rename($path, QUEUE_ROOT . '/pending/' . $targetName)) {
                throw new RuntimeException('could not recover published queue item');
            }
            ++$recovered;
            continue;
        }
        if (!rename($path, QUEUE_ROOT . '/failed/incomplete-' . $targetName)) {
            throw new RuntimeException('could not preserve incomplete queue item');
        }
        workerLog('error', 'incomplete_event_moved_to_dead_letter');
    }
    return $recovered;
}

function validEndpoint(string $endpoint): bool
{
    if (filter_var($endpoint, FILTER_VALIDATE_URL) === false) {
        return false;
    }
    $parts = parse_url($endpoint);
    if (!is_array($parts) || !in_array($parts['scheme'] ?? '', ['http', 'https'], true) || empty($parts['host'])) {
        return false;
    }
    return preg_match('~/postback/[^/]+$~', (string) ($parts['path'] ?? '')) === 1;
}

function validEvent($event): bool
{
    return is_array($event)
        && validEndpoint((string) ($event['endpoint'] ?? ''))
        && isset($event['code'], $event['ad_id'], $event['subid'])
        && $event['code'] !== ''
        && ctype_digit((string) $event['ad_id'])
        && $event['subid'] !== '';
}

try {
    foreach (['tmp', 'pending', 'processing', 'retry', 'failed'] as $directory) {
        if (!is_dir(QUEUE_ROOT . '/' . $directory)) {
            throw new RuntimeException('queue directory is unavailable: ' . $directory);
        }
    }
    $lock = fopen(QUEUE_ROOT . '/worker.lock', 'c');
    if ($lock === false || !flock($lock, LOCK_EX | LOCK_NB)) {
        throw new RuntimeException('another worker is already running');
    }

    $recovered = recoverPublisherFiles();
    $recovered += recoverQueueFiles(QUEUE_ROOT . '/processing', QUEUE_ROOT . '/pending');
    $recovered += recoverQueueFiles(QUEUE_ROOT . '/tmp', QUEUE_ROOT . '/pending');
    workerLog('info', 'worker_started', ['version' => WORKER_VERSION, 'recovered' => $recovered]);

    while (true) {
        recoverPublisherFiles();
        recoverQueueFiles(QUEUE_ROOT . '/tmp', QUEUE_ROOT . '/pending');
        $pending = array_merge(
            dueRetryFiles(RETRY_BATCH_SIZE, BATCH_SIZE * 20),
            queuedFiles(QUEUE_ROOT . '/pending', BATCH_SIZE)
        );
        $items = [];
        foreach ($pending as $path) {
            if (count($items) >= BATCH_SIZE) {
                break;
            }
            $event = json_decode((string) file_get_contents($path), true);
            if (!validEvent($event)) {
                moveEvent($path, QUEUE_ROOT . '/failed');
                workerLog('error', 'invalid_event_moved_to_dead_letter');
                continue;
            }
            if ((int) ($event['next_attempt_at'] ?? 0) > time()) {
                continue;
            }
            $claimed = moveEvent($path, QUEUE_ROOT . '/processing');
            $items[] = ['path' => $claimed, 'event' => $event];
        }
        if ($items === []) {
            usleep(250000);
            continue;
        }
        foreach (deliver($items) as $result) {
            $logContext = array_merge([
                'code' => (string) ($result['item']['event']['code'] ?? ''),
                'http_code' => $result['http_code'],
                'curl_errno' => $result['curl_errno'],
                'error' => $result['error'],
            ], $result['profile']);
            if ($result['ok']) {
                if (($result['item']['event']['diagnostics'] ?? false) === true) {
                    workerLog('info', 'delivery_profile', $logContext);
                }
                if (!unlink($result['item']['path'])) {
                    throw new RuntimeException('could not acknowledge delivered queue item');
                }
                continue;
            }
            workerLog('warning', 'delivery_failed', $logContext);
            retryEvent($result['item']['path'], $result['item']['event']);
        }
    }
} catch (\Throwable $error) {
    workerLog('critical', 'worker_stopped', [
        'type' => get_class($error),
        'message' => substr($error->getMessage(), 0, 300),
    ]);
    fwrite(STDERR, '[LazyArb worker] ' . $error->getMessage() . PHP_EOL);
    exit(1);
}
