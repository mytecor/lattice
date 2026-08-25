# F2. Reticulum поверх TCP/IP и удалённый доступ

Узлы связываются через интернет, а не только в LAN, и доступны администратору по RNS-адресу
даже за NAT. Соответствует [вехе 2](../VISION.md#вехи-порядок-без-деталей).

Задачи: [f2-01](../tasks/f2-01-reticulum-tcp-interfaces.md),
[f2-02](../tasks/f2-02-define-entry-points.md),
[f2-03](../tasks/f2-03-second-node-rnsh.md),
[f2-04](../tasks/f2-04-rnsh-nat-access.md).

**Критерий готовности:** с ноутбука через rnsh открывается shell на узле за NAT, связь переживает
смену IP на стороне клиента.
