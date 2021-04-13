package tree

/**
 * @Author: lbh
 * @Date: 2021/4/13
 * @Description: gin源码解析——tree.go核心逻辑代码
 */

// 该前缀树实现的核心代码:
// addRoute (第87行)
// insertChild (第358行)

import (
	"strings"
	"unsafe"
)

// 为了不报错 先简单定义
type HandlersChain func()

type nodeType uint8

const (
	static   nodeType = iota // 默认类型
	root                     // 根结点
	param                    // 带参数(":")
	catchAll                 // 匹配所有("*")
)

type node struct {
	path      string        //当前结点储存的路径
	indices   string        //当前结点所有子结点的path首字符
	wildChild bool          //当前结点的子结点是否为模糊结点(带":"或"*")
	nType     nodeType      //当前结点的类型
	priority  uint32        //当前结点的权重
	children  []*node       //当前结点的孩子结点列表
	handlers  HandlersChain //当前结点对应的处理函数(若不是完整路径，则为nil)
	fullPath  string        //从根结点到当前结点的完整路径
}

//min of a and b
func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

//byte to string
func BytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// Increments priority of the given child and reorders if necessary
// 处理、调整子结点们的优先级(非核心功能，仅为增强匹配速率)
func (n *node) incrementChildPrio(pos int) int {
	cs := n.children
	cs[pos].priority++
	prio := cs[pos].priority

	// Adjust position (move to front)
	newPos := pos
	for ; newPos > 0 && cs[newPos-1].priority < prio; newPos-- {
		// Swap node positions
		cs[newPos-1], cs[newPos] = cs[newPos], cs[newPos-1]
	}

	// Build new index char string
	if newPos != pos {
		n.indices = n.indices[:newPos] + // Unchanged prefix, might be empty
			n.indices[pos:pos+1] + // The index char we move
			n.indices[newPos:pos] + n.indices[pos+1:] // Rest without char at 'pos'
	}

	return newPos
}

// 获取两个字符串的最大公共前缀长度
func longestCommonPrefix(a, b string) int {
	i := 0
	max := min(len(a), len(b))
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}

//添加路由
func (n *node) addRoute(path string, handlers HandlersChain) {
	//传入的路径是全路径
	fullPath := path
	n.priority++

	// 如果是空树那么当前结点就变成根结点
	if len(n.path) == 0 && len(n.children) == 0 {
		n.insertChild(path, fullPath, handlers)
		n.nType = root
		return
	}

	//记录整个过程中当前"path"(部分路径)位于全路径的什么位置
	parentFullPathIndex := 0

	// loop
	// 相当于将这个"for"命名为"walk"
	// 这样无论里面嵌套多少循环
	// 都可以通过"continue wall"回到最外层循环
	// 类似于 "goto"
walk:
	for {
		// 获取公共前缀长度
		// 公共前缀不包含":"和"*"
		i := longestCommonPrefix(path, n.path)

		// 1. 先有  /namespace (n.path) 再插入 /name (path)
		// 2. 先有  /name (n.path) 再插入 /namespace (path)
		// 是两种不一样的情况(但都是一个是另一个是子串)
		// 第一种情况只要把 /namespace (n.path)分裂即可
		// 第二种情况只要新插入一个结点 space在/name(n.path)后面即可
		// 两种情况都还包括: 先有/name 再有/need (一个不是另一个的子串)
		// 但第一种情况处理后，第二种情况下，n.path一定是path的子串
		// 因为n只留下了公共前缀，剩下的变成了n的子结点

		// 如果公共前缀长度小于当前结点长度
		// 对应第一种情况
		// 这种情况并不对新结点执行插入操作
		// 只对n执行分裂的操作
		if i < len(n.path) {

			// 要把当前结点分裂成两部分
			// 第一部分是公共前缀 第二部分是剩余子串
			// 第一部分连接第二部分(是第二部分的父节点)
			// 第二部分要继续连接子结点们
			// 所以新建一个结点用来保存第二部分
			// 新结点继承了原结点的大部分属性
			child := node{
				path:      n.path[i:], //新结点保存的是 原结点公共前缀后的部分
				wildChild: n.wildChild,
				indices:   n.indices,
				children:  n.children,
				handlers:  n.handlers,
				priority:  n.priority - 1, //由于变成子结点了，相当于权重降了一级
				fullPath:  n.fullPath,
			}

			// 现在原结点的孩子结点变成了新结点
			n.children = []*node{&child}
			// 现在原结点保存新结点的首字母
			n.indices = BytesToString([]byte{n.path[i]})
			// 现在原结点的path变成了公共前缀
			n.path = path[:i]
			// 现在原结点的handlers先定义成nil 最终会统一赋值
			n.handlers = nil
			// 现在原结点的子结点(原结点的第二部分)一定不是通配结点
			// 因为路径中间不能出现":"和"*"
			n.wildChild = false
			// 现在原结点的全路径变成了公共前缀最后一个字符前的全路径
			n.fullPath = fullPath[:parentFullPathIndex+i]
		}

		// 如果公共前缀长度小于path长度
		// 对应第二种情况
		// 经过第一种情况以后
		// n.path一定是path是子串(否则不会进入第二种情况)
		// 即n.path是path的前缀
		// 这里要考虑参数结点的各种情况
		if i < len(path) {
			// path变成公共前缀后的部分
			path = path[i:]
			// 取path第一个字符
			c := path[0]

			// '/' after param
			// 参数结点(":"类型)后还有其他路径
			// 如:   n.path = /:name
			// 新来的   path = /:name/home (第一种情况后 n.path一定是path的子串)
			// 那么经过 path = path[i:]操作后
			// path变成了 /home   c=path[0]=/
			// 此时n是参数结点
			// 注意: gin里面参数结点最少会有一个孩子结点'/'
			// 正是因为参数结点这个特殊性
			// 所以才需要这个特殊情况来判别
			// 这种情况其实就是向没有"有效子结点"的参数结点中插入子结点
			// (默认的那个'/'结点不算是"有效子结点")
			if n.nType == param && c == '/' && len(n.children) == 1 {
				parentFullPathIndex += len(n.path)
				//移到下一级结点后，继续循环
				n = n.children[0]
				n.priority++
				continue walk
			}

			// Check if a child with the next path byte exists
			// 如果不符合上面的特殊情况
			// 那么进入更一般的情况
			// 由于此时path已经取了公共前缀后的部分
			// 而n.path则是前缀部分
			// 那么path需要插入为n.path的子结点(这里是与其子结点分裂合并)
			// 先遍历结点n储存的子结点们的首字符indices
			// 看看有没有首字符与新结点首字符相同的
			for i, max := 0, len(n.indices); i < max; i++ {

				//如果有
				if c == n.indices[i] {
					parentFullPathIndex += len(n.path)
					//先处理权重(非核心功能，仅为了优化匹配速率)
					i = n.incrementChildPrio(i)
					//然后path与该结点重新进行分裂合并，即重新循环
					n = n.children[i]
					continue walk
				}
			}

			// Otherwise insert it
			// 如果不符合上面的情况
			// 说明n的子结点中没有与path首字符相同的
			// 无法进行分裂合并
			// 那么如果path不是通配结点(如果是通配结点那么要另外考虑n是否也为通配结点)
			// 并且n的类型不是"catchAll"(*类型的结点不能有子结点)时
			// 将path插入为n的子结点
			// 先处理最简单的情况
			if c != ':' && c != '*' && n.nType != catchAll {
				// []byte for proper unicode char conversion, see #65
				// 拼接path第一个字符到n.indices中
				n.indices += BytesToString([]byte{c})
				child := &node{
					fullPath: fullPath,
				}
				n.addChild(child)
				n.incrementChildPrio(len(n.indices) - 1)
				n = child
			} else if n.wildChild {
				// 不符合上面的条件 说明 path是通配结点||n是*类型的通配结点
				// 如果n的孩子结点是通配结点("/"或"*")
				// 由于不能有其他结点与通配结点处于同一级
				// (如 /user/:name  和  /user/test
				// 这种情况 :name 和 test处于同一级
				// 那么访问不到 test 因为匹配时这一级的字符串会被当作给参数 :name 赋值)
				// 即n只有一个子结点 就是这个通配结点
				// 那么切换到这个结点
				// inserting a wildcard node, need to check if it conflicts with the existing wildcard
				n = n.children[len(n.children)-1]
				n.priority++

				// Check if the wildcard matches
				// 第一行是判断n.path是否为path的子串
				// 此时的n已经切换到子结点了 已经可以确定的是n是一个通配结点
				if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&

					// Adding a child to a catchAll is not possible
					// 第二行是禁止给*类型结点加子结点
					// n不能为*类型结点 因为*类型结点不能有子结点(否则访问不到)
					// 也不能有同级结点
					n.nType != catchAll &&

					// Check for longer wildcard, e.g. :name and :names
					// 第三行判断会产生矛盾
					//
					// 如果: len(n.path) >= len(path) 为true
					// 由于: len(path) >= len(n.path) 为true
					// 那么二者长度相等 而上面已经判断了两个字符串匹配(第一行)
					// 那么两个字符串相同 进行下一轮循环即可
					//
					// 或者path[len(n.path)] == '/'
					// 如果这个 说明n.path<path(否则不会判断这个条件)
					// 那么说明n.path是path的前缀 且前缀后的第一个字符为'/'
					// 例如 path:   /:name/walk
					//    n.path:  /:name
					// 那么说明是要在n这个参数结点下插入子结点
					// 那么继续循环即可
					(len(n.path) >= len(path) || path[len(n.path)] == '/') {
					continue walk
				}

				// Wildcard conflict
				// 通配符冲突了
				// 有几种可能:
				// 1. n.path:  /:name
				//      path:  /:names (都是参数结点但是参数名不一样)
				// 2. n是*类型结点       (插入任意结点或与任意结点同级)
				// 3. n.path:  /:name
				//      path:  /name (参数结点与非参数结点同级)
				pathSeg := path
				if n.nType != catchAll {
					pathSeg = strings.SplitN(pathSeg, "/", 2)[0]
				}
				prefix := fullPath[:strings.Index(fullPath, pathSeg)] + n.path
				panic("'" + pathSeg +
					"' in new path '" + fullPath +
					"' conflicts with existing wildcard '" + n.path +
					"' in existing prefix '" + prefix +
					"'")
			}

			// 至此 已经判断完了全部条件
			// n已经是(可能经过了分裂合并)后与path没有任何公共前缀的结点了
			// 将path插入为n的子结点
			n.insertChild(path, fullPath, handlers)
			return
		}

		// Otherwise add handle to current node
		// 如果程序走到这里
		// 说明没有在第二种情况中return
		// 那唯一可能就是没有进入第二种情况 否则一定会panic或return
		// 也就是从第一种情况直接过来的
		// 注意 第一种情况并没有进行切换结点操作(没有进行n=n.children[0])
		// 如 n.path = /namespace     path = /name
		// 那么现在n.path = /name 且space为其子结点
		// space继承了原本n的大多属性 包括handlers
		// 这里要对handlers进行设置(因为/name)也有对应方法了
		if n.handlers != nil {
			panic("handlers are already registered for path '" + fullPath + "'")
		}
		n.handlers = handlers
		n.fullPath = fullPath
		return
	}
}

// 查找path中是否有通配符
// 返回:
// wildcard  通配符结点"参数名"
// i         通配符结点起始位置索引
// valid     是否有效
// 如:  path = /user/:name/home
// 则:  wildcard = :name   i = 6  valid = true
//
// 如:  path = /user/:name*/home
// 则:  wildcard = :name*  i = 6  valid = false
// 函数内部逻辑很明显 不另加注释
func findWildcard(path string) (wildcard string, i int, valid bool) {
	// Find start
	for start, c := range []byte(path) {
		// A wildcard starts with ':' (param) or '*' (catch-all)
		if c != ':' && c != '*' {
			continue
		}

		// Find end and check for invalid characters
		valid = true
		for end, c := range []byte(path[start+1:]) {
			switch c {
			case '/':
				return path[start : start+1+end], start, valid
			case ':', '*':
				valid = false
			}
		}
		return path[start:], start, valid
	}
	return "", -1, false
}

// 在n结点下插入孩子结点
func (n *node) insertChild(path string, fullPath string, handlers HandlersChain) {
	for {
		// Find prefix until first wildcard
		wildcard, i, valid := findWildcard(path)
		if i < 0 { // No wildcard found
			break
		}

		// The wildcard name must not contain ':' and '*'
		if !valid {
			panic("only one wildcard per path segment is allowed, has: '" +
				wildcard + "' in path '" + fullPath + "'")
		}

		// check if the wildcard has a name
		if len(wildcard) < 2 {
			panic("wildcards must be named with a non-empty name in path '" + fullPath + "'")
		}

		if wildcard[0] == ':' { // param
			if i > 0 {
				// Insert prefix before the current wildcard
				n.path = path[:i]
				path = path[i:]
			}

			child := &node{
				nType:    param,
				path:     wildcard,
				fullPath: fullPath,
			}
			n.addChild(child)
			n.wildChild = true
			n = child
			n.priority++

			// if the path doesn't end with the wildcard, then there
			// will be another non-wildcard subpath starting with '/'
			if len(wildcard) < len(path) {
				path = path[len(wildcard):]

				child := &node{
					priority: 1,
					fullPath: fullPath,
				}
				n.addChild(child)
				n = child
				continue
			}

			// Otherwise we're done. Insert the handle in the new leaf
			n.handlers = handlers
			return
		}

		// catchAll
		if i+len(wildcard) != len(path) {
			panic("catch-all routes are only allowed at the end of the path in path '" + fullPath + "'")
		}

		if len(n.path) > 0 && n.path[len(n.path)-1] == '/' {
			panic("catch-all conflicts with existing handle for the path segment root in path '" + fullPath + "'")
		}

		// currently fixed width 1 for '/'
		i--
		if path[i] != '/' {
			panic("no / before catch-all in path '" + fullPath + "'")
		}

		n.path = path[:i]

		// First node: catchAll node with empty path
		child := &node{
			wildChild: true,
			nType:     catchAll,
			fullPath:  fullPath,
		}

		n.addChild(child)
		n.indices = string('/')
		n = child
		n.priority++

		// second node: node holding the variable
		child = &node{
			path:     path[i:],
			nType:    catchAll,
			handlers: handlers,
			priority: 1,
			fullPath: fullPath,
		}
		n.children = []*node{child}

		return
	}

	// If no wildcard was found, simply insert the path and handle
	n.path = path
	n.handlers = handlers
	n.fullPath = fullPath
}

// addChild will add a child node, keeping wildcards at the end
func (n *node) addChild(child *node) {
	if n.wildChild && len(n.children) > 0 {
		wildcardChild := n.children[len(n.children)-1]
		n.children = append(n.children[:len(n.children)-1], child, wildcardChild)
	} else {
		n.children = append(n.children, child)
	}
}
