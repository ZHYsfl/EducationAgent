phase 1

1677 - 1982 phase1 voice agent collect requirements, ppt agent has not started.  interrupt entropy as large as possible(305条)

1983 - 2207 chat dataset(no action) no interrupt (225条) 

2208 - 2469 chat dataset(no action) interrupt (262条) # 问题: 这里实际上划分不应该是interrupt和no interrupt而是phase1和phase2不同阶段prompt前缀的纯对话，所以应该把数据各自分一半然后给不同阶段的prompt?

phase 2

1201 - 1279 not interrupted chat dataset(include action) : queue is not empty : the new version of the ppt is generated successfully (79条)

1280 - 1364 not interrupted chat dataset(include action) : queue is not empty : question for user (85条)

1365 - 1526 not interrupted chat dataset(include action): mixed : question for user(162条)



1527 -  1541 interrupted chat dataset(include action) : queue is not empty : question for user 一次打断 (15条)

1542 - 1556 interrupted chat dataset(include action) : queue is not empty : question for user 两次打断(15条)

1557 - 1571 interrupted chat dataset(include action) : queue is not empty : question for user 三次打断(15条)

1572 - 1586 interrupted chat dataset(include action) : queue is not empty : question for user 四次打断(15条)

1587 - 1601 interrupted chat dataset(include action) : queue is not empty : question for user 五次打断(15条)



1602 - 1616 interrupted chat dataset(include action) : queue is not empty : new version of the ppt is generated successfully 一次打断(15条)

1617 - 1631 interrupted chat dataset(include action) : queue is not empty : new version of the ppt is generated successfully 两次打断(15条)

1632 - 1646 interrupted chat dataset(include action) : queue is not empty : new version of the ppt is generated successfully 三次打断(15条)

1647 - 1661 interrupted chat dataset(include action) : queue is not empty : new version of the ppt is generated successfully 四次打断(15条)

1662 - 1676 interrupted chat dataset(include action) : queue is not empty : new version of the ppt is generated successfully 五次打断(15条)