const StepOne = () => {
    return (
        <div class="mt-32 max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div class="max-w-4xl mx-auto">
          <div class="md:flex md:items-center md:justify-between">
            <div class="flex-1 min-w-0">
              <h2 class="text-2xl font-bold leading-7 text-gray-900 sm:text-3xl sm:truncate mb-12">Gimlet Installer</h2>
            </div>
          </div>
          <nav aria-label="Progress">
            <ol class="border border-gray-300 rounded-md divide-y divide-gray-300 md:flex md:divide-y-0">
              <li class="relative md:flex-1 md:flex">
                {/* <!-- Current Step --> */}
                <div class="px-6 py-4 flex items-center text-sm font-medium select-none cursor-default" aria-current="step">
                  <span
                    class="flex-shrink-0 w-10 h-10 flex items-center justify-center border-2 border-indigo-600 rounded-full">
                    <span class="text-indigo-600">01</span>
                  </span>
                  <span class="ml-4 text-sm font-medium text-indigo-600">Create Github Application</span>
                </div>
  
                {/* <!-- Arrow separator for lg screens and up --> */}
                <div class="hidden md:block absolute top-0 right-0 h-full w-5" aria-hidden="true">
                  <svg class="h-full w-full text-gray-300" viewBox="0 0 22 80" fill="none" preserveAspectRatio="none">
                    <path d="M0 -2L20 40L0 82" vector-effect="non-scaling-stroke" stroke="currentcolor"
                      stroke-linejoin="round" />
                  </svg>
                </div>
              </li>
  
              <li class="relative md:flex-1 md:flex">
                {/* <!-- Upcoming Step --> */}
                <div class="group flex items-center select-none cursor-default">
                  <span class="px-6 py-4 flex items-center text-sm font-medium">
                    <span
                      class="flex-shrink-0 w-10 h-10 flex items-center justify-center border-2 border-gray-300 rounded-full">
                      <span class="text-gray-500">02</span>
                    </span>
                    <span class="ml-4 text-sm font-medium text-gray-500">Prepare gitops repository</span>
                  </span>
                </div>
  
                {/* <!-- Arrow separator for lg screens and up --> */}
                <div class="hidden md:block absolute top-0 right-0 h-full w-5" aria-hidden="true">
                  <svg class="h-full w-full text-gray-300" viewBox="0 0 22 80" fill="none" preserveAspectRatio="none">
                    <path d="M0 -2L20 40L0 82" vector-effect="non-scaling-stroke" stroke="currentcolor"
                      stroke-linejoin="round" />
                  </svg>
                </div>
              </li>
  
              <li class="relative md:flex-1 md:flex">
                {/* <!-- Upcoming Step --> */}
                <div class="group flex items-center select-none cursor-default">
                  <span class="px-6 py-4 flex items-center text-sm font-medium">
                    <span
                      class="flex-shrink-0 w-10 h-10 flex items-center justify-center border-2 border-gray-300 rounded-full">
                      <span class="text-gray-500">03</span>
                    </span>
                    <span class="ml-4 text-sm font-medium text-gray-500">Bootstrap gitops automation</span>
                  </span>
                </div>
              </li>
            </ol>
          </nav>
  
          <div class="mt-8 font-mono text-sm">
            <p class="">Gimlet Dashboard uses a Github Application to gain access to your source code.</p>
            <p class="">You must create this application first.</p>
  
            <p class="pt-8">Please note that this application will be owned by you, thus you don't give access to any third party or the makers of Gimlet.</p>
  
            <form action="https://github.com/settings/apps/new" method="post">
              <input type="hidden" name="manifest" id="manifest"></input><br />
              <input type="submit" value="Create Github app"
                class="cursor-pointer font-sans inline-flex items-center px-4 py-2 border border-transparent text-base font-medium rounded-md shadow-sm text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500">
              </input>
            </form>
          </div>
  
        </div>
      </div>
    );
};

export default StepOne;
