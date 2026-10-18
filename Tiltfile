# Local Kubernetes development. Migrations run before the application starts.
allow_k8s_contexts('docker-desktop')
config.define_bool('observability', usage='Run Grafana, Prometheus, Tempo, Loki and Alloy (default: true)')
config.define_bool('extra-brokers', usage='Run unused RabbitMQ and NATS for experiments')
config.define_bool('storage', usage='Run MinIO and enable the development object-storage disk (default: true)')
settings = config.parse()
storage = settings.get('storage', True)
observability = settings.get('observability', True)
k8s_yaml('deployments/development/kubernetes/namespace.yaml')
k8s_yaml('deployments/development/kubernetes/secrets.yaml')
# Gitignored local application credentials; required by the full feature profile.
if os.path.exists('.env.mail.yaml'):
    k8s_yaml('.env.mail.yaml')
app_config = decode_yaml(read_file('deployments/development/kubernetes/app-config.yaml'))
if not storage:
    app_config['data']['FILESYSTEM_DISK'] = 'disabled'
if not observability:
    app_config['data'].update({
        'LOG_CHANNELS': 'stderr,file',
        'METRICS_ENABLED': 'false',
        'TRACING_ENABLED': 'false',
    })
k8s_yaml(encode_yaml(app_config))
k8s_yaml('deployments/development/kubernetes/postgres-pvc.yaml')
k8s_yaml('deployments/development/kubernetes/postgres-deployment.yaml')
k8s_resource('postgres', port_forwards=['5442:5432'], labels=['database'])
k8s_yaml('deployments/development/kubernetes/redis-pvc.yaml')
k8s_yaml('deployments/development/kubernetes/redis-deployment.yaml')
k8s_resource('redis', port_forwards=['6379:6379'], labels=['brokers'])
if settings.get('extra-brokers', False):
    k8s_yaml('deployments/development/kubernetes/rabbitmq-config.yaml')
    k8s_yaml('deployments/development/kubernetes/rabbitmq-pvc.yaml')
    k8s_yaml('deployments/development/kubernetes/rabbitmq-deployment.yaml')
    k8s_resource('rabbitmq', port_forwards=['5672:5672', '15672:15672'], labels=['brokers'])
    k8s_yaml('deployments/development/kubernetes/nats-deployment.yaml')
    k8s_resource('nats', port_forwards=['4222:4222', '8222:8222'], labels=['brokers'])
if storage:
    docker_build('go-modular-minio', '.', dockerfile='deployments/development/docker/minio.Dockerfile', only=['deployments/development/docker/minio.Dockerfile'])
    k8s_yaml('deployments/development/kubernetes/minio.yaml')
    k8s_resource('minio', port_forwards=['9000:9000', '9001:9001'], labels=['storage'])
docker_build('go-modular-app', '.', dockerfile='deployments/Dockerfile')
# Docker Desktop kubeadm exposes shared Mac folders directly to its node.
local('mkdir -p storage/logs storage/app', quiet=True)
app_resources = decode_yaml_stream(read_file('deployments/development/kubernetes/app-deployment.yaml'))
app_resources += decode_yaml_stream(read_file('deployments/development/kubernetes/scheduler-deployment.yaml'))
for resource in app_resources:
    if resource['kind'] == 'Deployment':
        resource['spec']['template']['metadata'].setdefault('annotations', {})['go-modular/observability'] = str(observability)
        pod = resource['spec']['template']['spec']
        if not storage:
            pod['initContainers'] = [container for container in pod['initContainers'] if container['name'] != 'minio-init']
        pod['securityContext']['runAsUser'] = int(str(local('id -u', quiet=True)).strip())
        pod['securityContext']['runAsGroup'] = int(str(local('id -g', quiet=True)).strip())
        pod['securityContext']['fsGroup'] = pod['securityContext']['runAsGroup']
        for volume in pod['volumes']:
            if volume['name'] == 'storage':
                volume['hostPath']['path'] = os.path.join(os.getcwd(), 'storage')
if observability:
    for resource in app_resources:
        if resource['kind'] == 'Service':
            resource['spec']['ports'].append({'name': 'metrics', 'port': 9090, 'targetPort': 9090})
k8s_yaml(encode_yaml_stream(app_resources))
k8s_resource(
    'api',
    port_forwards=['8080:8080'],
    resource_deps=['postgres', 'redis'] + (['minio'] if storage else []),
    labels=['services']
)

k8s_resource(
    'scheduler',
    resource_deps=['api'],
    labels=['services']
)

if observability:
    telemetry_resources = decode_yaml_stream(kustomize('deployments/observability'))
    k8s_yaml(encode_yaml_stream(telemetry_resources))
    k8s_resource('grafana', port_forwards=['3000:3000'], labels=['observability'])
    k8s_resource('alloy', port_forwards=['4318:4318'], labels=['observability'])
    for name in ['prometheus', 'tempo', 'loki']:
        k8s_resource(name, labels=['observability'])
